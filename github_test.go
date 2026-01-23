package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v81/github"
)

func init() {
	logger = NewLogger(io.Discard)
}

func TestBuildTreeEntries(t *testing.T) {
	change := Change{
		entries: map[string][]byte{
			"a-file":  []byte("hello world"),
			"b-empty": {},
			"deleted": nil,
		},
	}

	// Build tree entries the same way PushChange does (without making API calls)
	var addedPaths, deletedPaths []string
	for path, content := range change.entries {
		if content == nil {
			deletedPaths = append(deletedPaths, path)
		} else {
			addedPaths = append(addedPaths, path)
		}
	}

	slices.Sort(addedPaths)

	if len(addedPaths) != 2 {
		t.Errorf("expected 2 added changes, got %d", len(addedPaths))
	} else if addedPaths[0] != "a-file" || addedPaths[1] != "b-empty" {
		t.Errorf("expected 'a-file' and 'b-empty' to be added, but got %q", addedPaths)
	}

	if len(deletedPaths) != 1 {
		t.Errorf("expected 1 deleted change, got %d", len(deletedPaths))
	} else if deletedPaths[0] != "deleted" {
		t.Errorf("expected deleted path to be 'deleted', got %s", deletedPaths[0])
	}
}

// newTestClient creates a Client configured to use the provided httptest server.
func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	ghClient := github.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(server.URL + "/")

	return &Client{
		repos:  ghClient.Repositories,
		git:    ghClient.Git,
		owner:  "test-owner",
		repo:   "test-repo",
		branch: "test-branch",
	}
}

func TestGetHeadCommitHash(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/test-owner/test-repo/branches/test-branch" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(github.Branch{
				Commit: &github.RepositoryCommit{
					SHA: github.Ptr("abc123def456"),
				},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		sha, err := client.GetHeadCommitHash(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "abc123def456" {
			t.Errorf("expected sha 'abc123def456', got %q", sha)
		}
	})

	t.Run("branch not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"message": "Branch not found",
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.GetHeadCommitHash(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), ErrNoRemoteBranch.Error()) {
			t.Errorf("expected error to contain %q, got %q", ErrNoRemoteBranch.Error(), err.Error())
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.GetHeadCommitHash(context.Background())
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestCreateBranch(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.URL.Path != "/repos/test-owner/test-repo/git/refs" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}

			var req github.CreateRef
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			if req.Ref != "refs/heads/test-branch" {
				t.Errorf("expected ref 'refs/heads/test-branch', got %q", req.Ref)
			}
			if req.SHA != "parent-sha-123" {
				t.Errorf("expected sha 'parent-sha-123', got %q", req.SHA)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(github.Reference{
				Object: &github.GitObject{
					SHA: github.Ptr("parent-sha-123"),
				},
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		sha, err := client.CreateBranch(context.Background(), "parent-sha-123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "parent-sha-123" {
			t.Errorf("expected sha 'parent-sha-123', got %q", sha)
		}
	})

	t.Run("branch point does not exist", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(map[string]string{
				"message": "Reference does not exist",
			})
		}))
		defer server.Close()

		client := newTestClient(t, server)
		_, err := client.CreateBranch(context.Background(), "nonexistent-sha")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "422") {
			t.Errorf("expected error to mention 422, got %q", err.Error())
		}
	})
}

func TestPushChange(t *testing.T) {
	t.Run("dry run returns zero hash", func(t *testing.T) {
		client := &Client{
			owner:  "test-owner",
			repo:   "test-repo",
			branch: "test-branch",
			dryrun: true,
		}

		change := Change{
			hash:    "abc123",
			message: "Test commit",
			entries: map[string][]byte{
				"file.txt": []byte("content"),
			},
		}

		sha, err := client.PushChange(context.Background(), "head-sha", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should return zeros with same length as input hash
		if sha != "000000" {
			t.Errorf("expected '000000', got %q", sha)
		}
	})

	t.Run("successful push with file addition", func(t *testing.T) {
		var (
			getCommitCalled  bool
			createBlobCalled bool
			createTreeCalled bool
			commitCalled     bool
			updateRefCalled  bool
		)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				getCommitCalled = true
				json.NewEncoder(w).Encode(github.Commit{
					SHA: github.Ptr("parent-sha"),
					Tree: &github.Tree{
						SHA: github.Ptr("parent-tree-sha"),
					},
				})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				createBlobCalled = true
				var blob github.Blob
				json.NewDecoder(r.Body).Decode(&blob)
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{
					SHA: github.Ptr("blob-sha-123"),
				})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				createTreeCalled = true
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{
					SHA: github.Ptr("new-tree-sha"),
				})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				commitCalled = true
				var commit github.Commit
				json.NewDecoder(r.Body).Decode(&commit)
				if commit.GetMessage() != "Test commit" {
					t.Errorf("unexpected commit message: %s", commit.GetMessage())
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{
					SHA: github.Ptr("new-commit-sha"),
				})

			case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/"):
				updateRefCalled = true
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{
						SHA: github.Ptr("new-commit-sha"),
					},
				})

			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local-hash",
			message: "Test commit",
			entries: map[string][]byte{
				"file.txt": []byte("content"),
			},
		}

		sha, err := client.PushChange(context.Background(), "parent-sha", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "new-commit-sha" {
			t.Errorf("expected 'new-commit-sha', got %q", sha)
		}

		if !getCommitCalled {
			t.Error("GetCommit was not called")
		}
		if !createBlobCalled {
			t.Error("CreateBlob was not called")
		}
		if !createTreeCalled {
			t.Error("CreateTree was not called")
		}
		if !commitCalled {
			t.Error("CreateCommit was not called")
		}
		if !updateRefCalled {
			t.Error("UpdateRef was not called")
		}
	})

	t.Run("successful push with file deletion", func(t *testing.T) {
		var treeEntries []*github.TreeEntry

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					SHA: github.Ptr("parent-sha"),
					Tree: &github.Tree{
						SHA: github.Ptr("parent-tree-sha"),
					},
				})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				var req struct {
					BaseTree string              `json:"base_tree"`
					Tree     []*github.TreeEntry `json:"tree"`
				}
				json.NewDecoder(r.Body).Decode(&req)
				treeEntries = req.Tree
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{
					SHA: github.Ptr("new-tree-sha"),
				})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{
					SHA: github.Ptr("new-commit-sha"),
				})

			case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/"):
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{
						SHA: github.Ptr("new-commit-sha"),
					},
				})

			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local-hash",
			message: "Delete file",
			entries: map[string][]byte{
				"deleted-file.txt": nil, // nil means deletion
			},
		}

		_, err := client.PushChange(context.Background(), "parent-sha", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify that the tree entry for deletion has no SHA (which signals deletion)
		if len(treeEntries) != 1 {
			t.Fatalf("expected 1 tree entry, got %d", len(treeEntries))
		}
		if treeEntries[0].GetPath() != "deleted-file.txt" {
			t.Errorf("expected path 'deleted-file.txt', got %q", treeEntries[0].GetPath())
		}
		// For deletions, SHA should be nil/empty
		if treeEntries[0].SHA != nil && *treeEntries[0].SHA != "" {
			t.Errorf("expected nil/empty SHA for deletion, got %q", *treeEntries[0].SHA)
		}
	})

	t.Run("commit with body", func(t *testing.T) {
		var receivedMessage string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				var commit github.Commit
				json.NewDecoder(r.Body).Decode(&commit)
				receivedMessage = commit.GetMessage()
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{SHA: github.Ptr("commit-sha")})

			case r.Method == http.MethodPatch:
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("commit-sha")},
				})
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local",
			message: "Headline\n\nThis is the body\nwith multiple lines",
			entries: map[string][]byte{"file.txt": []byte("x")},
		}

		_, err := client.PushChange(context.Background(), "parent", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !strings.Contains(receivedMessage, "Headline") {
			t.Errorf("message should contain headline, got %q", receivedMessage)
		}
		if !strings.Contains(receivedMessage, "This is the body") {
			t.Errorf("message should contain body, got %q", receivedMessage)
		}
	})

	t.Run("get parent commit fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local",
			message: "Test",
			entries: map[string][]byte{"file.txt": []byte("x")},
		}

		_, err := client.PushChange(context.Background(), "nonexistent", change)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "get parent commit") {
			t.Errorf("expected 'get parent commit' error, got %q", err.Error())
		}
	})

	t.Run("create blob fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			if strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/") {
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})
				return
			}
			if r.URL.Path == "/repos/test-owner/test-repo/git/blobs" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local",
			message: "Test",
			entries: map[string][]byte{"file.txt": []byte("x")},
		}

		_, err := client.PushChange(context.Background(), "parent", change)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "create blob") {
			t.Errorf("expected 'create blob' error, got %q", err.Error())
		}
	})

	t.Run("create tree fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})
			case r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})
			case r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local",
			message: "Test",
			entries: map[string][]byte{"file.txt": []byte("x")},
		}

		_, err := client.PushChange(context.Background(), "parent", change)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "create tree") {
			t.Errorf("expected 'create tree' error, got %q", err.Error())
		}
	})

	t.Run("create commit fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})
			case r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})
			case r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				w.WriteHeader(http.StatusInternalServerError)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local",
			message: "Test",
			entries: map[string][]byte{"file.txt": []byte("x")},
		}

		_, err := client.PushChange(context.Background(), "parent", change)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "create commit") {
			t.Errorf("expected 'create commit' error, got %q", err.Error())
		}
	})

	t.Run("update ref fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})
			case r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})
			case r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{SHA: github.Ptr("commit-sha")})
			case r.Method == http.MethodPatch:
				w.WriteHeader(http.StatusConflict)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		change := Change{
			hash:    "local",
			message: "Test",
			entries: map[string][]byte{"file.txt": []byte("x")},
		}

		_, err := client.PushChange(context.Background(), "parent", change)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "update ref") {
			t.Errorf("expected 'update ref' error, got %q", err.Error())
		}
	})
}

func TestPushChanges(t *testing.T) {
	t.Run("multiple changes", func(t *testing.T) {
		commitCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})
			case r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})
			case r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				commitCount++
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{SHA: github.Ptr("commit-sha-" + string(rune('0'+commitCount)))})
			case r.Method == http.MethodPatch:
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("final-sha")},
				})
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		changes := []Change{
			{hash: "h1", message: "First", entries: map[string][]byte{"a.txt": []byte("a")}},
			{hash: "h2", message: "Second", entries: map[string][]byte{"b.txt": []byte("b")}},
			{hash: "h3", message: "Third", entries: map[string][]byte{"c.txt": []byte("c")}},
		}

		count, sha, err := client.PushChanges(context.Background(), "initial", changes...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 3 {
			t.Errorf("expected count 3, got %d", count)
		}
		if sha == "" {
			t.Error("expected non-empty sha")
		}
		if commitCount != 3 {
			t.Errorf("expected 3 commits to be created, got %d", commitCount)
		}
	})

	t.Run("failure on second change", func(t *testing.T) {
		commitCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})
			case r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})
			case r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				commitCount++
				if commitCount == 2 {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{SHA: github.Ptr("commit-sha")})
			case r.Method == http.MethodPatch:
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("sha")},
				})
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		changes := []Change{
			{hash: "h1", message: "First", entries: map[string][]byte{"a.txt": []byte("a")}},
			{hash: "h2", message: "Second", entries: map[string][]byte{"b.txt": []byte("b")}},
			{hash: "h3", message: "Third", entries: map[string][]byte{"c.txt": []byte("c")}},
		}

		count, _, err := client.PushChanges(context.Background(), "initial", changes...)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if count != 2 {
			t.Errorf("expected count 2 (failed on second), got %d", count)
		}
	})
}

func TestURLHelpers(t *testing.T) {
	client := &Client{
		owner:  "myorg",
		repo:   "myrepo",
		branch: "feature-branch",
	}

	t.Run("compareURL", func(t *testing.T) {
		url := client.compareURL("abc123", "def456")
		expected := "https://github.com/myorg/myrepo/compare/abc123...def456"
		if url != expected {
			t.Errorf("expected %q, got %q", expected, url)
		}
	})

	t.Run("commitURL", func(t *testing.T) {
		url := client.commitURL("abc123")
		expected := "https://github.com/myorg/myrepo/commit/abc123"
		if url != expected {
			t.Errorf("expected %q, got %q", expected, url)
		}
	})
}
