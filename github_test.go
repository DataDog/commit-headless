package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-github/v85/github"
)

func init() {
	logger = NewLogger(io.Discard)
}

// newFileEntry creates a FileEntry with default mode 100644
func newFileEntry(content []byte) FileEntry {
	return FileEntry{Content: content, Mode: "100644"}
}

// mockGraphQL implements GraphQLAPI for testing.
type mockGraphQL struct {
	handler func(query string, variables map[string]any) (json.RawMessage, error)
}

func (m *mockGraphQL) Do(_ context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	return m.handler(query, variables)
}

func signedGraphQLResponse(oid string) json.RawMessage {
	return json.RawMessage(`{"createCommitOnBranch":{"commit":{"oid":"` + oid + `","signature":{"isValid":true}}}}`)
}

func unsignedGraphQLResponse(oid string) json.RawMessage {
	return json.RawMessage(`{"createCommitOnBranch":{"commit":{"oid":"` + oid + `","signature":{"isValid":false}}}}`)
}

func TestBuildTreeEntries(t *testing.T) {
	change := Change{
		entries: map[string]FileEntry{
			"a-file":  newFileEntry([]byte("hello world")),
			"b-empty": newFileEntry([]byte{}),
			"deleted": {Content: nil}, // deletion
		},
	}

	// Build tree entries the same way prepTree does (without making API calls)
	var addedPaths, deletedPaths []string
	for path, fe := range change.entries {
		if fe.Content == nil {
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

func TestCreateChange(t *testing.T) {
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
			entries: map[string]FileEntry{
				"file.txt": newFileEntry([]byte("content")),
			},
		}

		sha, _, err := client.CreateChange(context.Background(), "", "head-sha", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "000000" {
			t.Errorf("expected '000000', got %q", sha)
		}
	})

	t.Run("graphql commit", func(t *testing.T) {
		callCount := 0
		client := &Client{
			owner:  "test-owner",
			repo:   "test-repo",
			branch: "test-branch",
			graphql: &mockGraphQL{
				handler: func(query string, variables map[string]any) (json.RawMessage, error) {
					callCount++
					input := variables["input"].(map[string]any)
					branch := input["branch"].(map[string]any)
					if branch["branchName"] != "tmp-branch" {
						t.Errorf("expected branch 'tmp-branch', got %v", branch["branchName"])
					}
					if input["expectedHeadOid"] != "parent-sha" {
						t.Errorf("expected expectedHeadOid 'parent-sha', got %v", input["expectedHeadOid"])
					}
					return signedGraphQLResponse("new-commit-sha"), nil
				},
			},
		}

		change := Change{
			hash:    "local-hash",
			message: "Test commit",
			entries: map[string]FileEntry{
				"file.txt": newFileEntry([]byte("content")),
			},
		}

		sha, _, err := client.CreateChange(context.Background(), "tmp-branch", "parent-sha", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "new-commit-sha" {
			t.Errorf("expected 'new-commit-sha', got %q", sha)
		}
		if callCount != 1 {
			t.Errorf("expected 1 GraphQL call, got %d", callCount)
		}
	})

	t.Run("rest fallback for non-default modes with non-user token", func(t *testing.T) {
		var (
			getCommitCalled  bool
			createBlobCalled bool
			createTreeCalled bool
			commitCalled     bool
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
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{
					SHA: github.Ptr("new-commit-sha"),
				})

			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		client.userToken = false // non-user token

		change := Change{
			hash:    "local-hash",
			message: "Test commit",
			entries: map[string]FileEntry{
				"script.sh": {Content: []byte("#!/bin/bash"), Mode: "100755"},
			},
		}

		sha, _, err := client.CreateChange(context.Background(), "tmp-branch", "parent-sha", change)
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
	})

	t.Run("graphql used for non-default modes with user token", func(t *testing.T) {
		client := &Client{
			owner:     "test-owner",
			repo:      "test-repo",
			branch:    "test-branch",
			userToken: true,
			graphql: &mockGraphQL{
				handler: func(query string, variables map[string]any) (json.RawMessage, error) {
					return signedGraphQLResponse("graphql-commit-sha"), nil
				},
			},
		}

		change := Change{
			hash:    "local-hash",
			message: "Test commit",
			entries: map[string]FileEntry{
				"script.sh": {Content: []byte("#!/bin/bash"), Mode: "100755"},
			},
		}

		sha, _, err := client.CreateChange(context.Background(), "tmp-branch", "parent-sha", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if sha != "graphql-commit-sha" {
			t.Errorf("expected 'graphql-commit-sha', got %q", sha)
		}
	})

	t.Run("commit with body via graphql", func(t *testing.T) {
		var receivedHeadline, receivedBody string

		client := &Client{
			owner:  "test-owner",
			repo:   "test-repo",
			branch: "test-branch",
			graphql: &mockGraphQL{
				handler: func(query string, variables map[string]any) (json.RawMessage, error) {
					input := variables["input"].(map[string]any)
					msg := input["message"].(map[string]string)
					receivedHeadline = msg["headline"]
					if b, ok := msg["body"]; ok {
						receivedBody = b
					}
					return signedGraphQLResponse("commit-sha"), nil
				},
			},
		}

		change := Change{
			hash:    "local",
			message: "Headline\n\nThis is the body\nwith multiple lines",
			entries: map[string]FileEntry{"file.txt": newFileEntry([]byte("x"))},
		}

		_, _, err := client.CreateChange(context.Background(), "tmp", "parent", change)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if receivedHeadline != "Headline" {
			t.Errorf("expected headline 'Headline', got %q", receivedHeadline)
		}
		if !strings.Contains(receivedBody, "This is the body") {
			t.Errorf("expected body to contain 'This is the body', got %q", receivedBody)
		}
	})

	t.Run("rest get parent commit fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		client := newTestClient(t, server)
		client.userToken = false
		change := Change{
			hash:    "local",
			message: "Test",
			entries: map[string]FileEntry{"script.sh": {Content: []byte("x"), Mode: "100755"}},
		}

		_, _, err := client.CreateChange(context.Background(), "tmp", "nonexistent", change)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "get parent commit") {
			t.Errorf("expected 'get parent commit' error, got %q", err.Error())
		}
	})
}

func TestPushChanges(t *testing.T) {
	t.Run("graphql with throwaway branch", func(t *testing.T) {
		var (
			createdRefs []string
			deletedRefs []string
			updatedRefs []string
		)
		commitCount := 0

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
				var req github.CreateRef
				json.NewDecoder(r.Body).Decode(&req)
				createdRefs = append(createdRefs, req.Ref)
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("initial")},
				})

			case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/"):
				ref := strings.TrimPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/")
				updatedRefs = append(updatedRefs, ref)
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("final-sha")},
				})

			case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/"):
				ref := strings.TrimPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/")
				deletedRefs = append(deletedRefs, ref)
				w.WriteHeader(http.StatusNoContent)

			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		client.graphql = &mockGraphQL{
			handler: func(query string, variables map[string]any) (json.RawMessage, error) {
				commitCount++
				return signedGraphQLResponse("commit-sha-" + string(rune('0'+commitCount))), nil
			},
		}

		changes := []Change{
			{hash: "h1", message: "First", entries: map[string]FileEntry{"a.txt": newFileEntry([]byte("a"))}},
			{hash: "h2", message: "Second", entries: map[string]FileEntry{"b.txt": newFileEntry([]byte("b"))}},
		}

		count, sha, err := client.PushChanges(context.Background(), "initial", changes...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 2 {
			t.Errorf("expected count 2, got %d", count)
		}
		if sha == "" {
			t.Error("expected non-empty sha")
		}

		// Should have created a throwaway branch
		if len(createdRefs) != 1 {
			t.Fatalf("expected 1 created ref, got %d", len(createdRefs))
		}
		if !strings.Contains(createdRefs[0], "--headless-tmp-") {
			t.Errorf("expected throwaway branch ref, got %q", createdRefs[0])
		}

		// Should have updated only the real branch
		if len(updatedRefs) != 1 {
			t.Fatalf("expected 1 updated ref, got %d: %v", len(updatedRefs), updatedRefs)
		}
		if updatedRefs[0] != "heads/test-branch" {
			t.Errorf("expected real branch update, got %q", updatedRefs[0])
		}

		// Should have deleted the throwaway branch
		if len(deletedRefs) != 1 {
			t.Fatalf("expected 1 deleted ref, got %d", len(deletedRefs))
		}
		if !strings.Contains(deletedRefs[0], "--headless-tmp-") {
			t.Errorf("expected throwaway branch deletion, got %q", deletedRefs[0])
		}

		if commitCount != 2 {
			t.Errorf("expected 2 GraphQL commits, got %d", commitCount)
		}
	})

	t.Run("rest fallback advances throwaway branch", func(t *testing.T) {
		var updatedRefs []string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("initial")},
				})

			case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
				json.NewEncoder(w).Encode(github.Commit{
					Tree: &github.Tree{SHA: github.Ptr("tree-sha")},
				})

			case r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/trees":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})

			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/commits":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Commit{SHA: github.Ptr("rest-commit-sha")})

			case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/"):
				ref := strings.TrimPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/refs/")
				updatedRefs = append(updatedRefs, ref)
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("rest-commit-sha")},
				})

			case r.Method == http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)

			default:
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		client.userToken = false

		changes := []Change{
			{hash: "h1", message: "Exec file", entries: map[string]FileEntry{"script.sh": {Content: []byte("#!/bin/bash"), Mode: "100755"}}},
		}

		count, _, err := client.PushChanges(context.Background(), "initial", changes...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 1 {
			t.Errorf("expected count 1, got %d", count)
		}

		// Should have updated both the throwaway branch (to sync) and the real branch
		if len(updatedRefs) != 2 {
			t.Fatalf("expected 2 updated refs, got %d: %v", len(updatedRefs), updatedRefs)
		}

		// First update should be the throwaway branch sync
		if !strings.Contains(updatedRefs[0], "--headless-tmp-") {
			t.Errorf("expected first ref update to be throwaway branch, got %q", updatedRefs[0])
		}
		// Second update should be the real branch
		if updatedRefs[1] != "heads/test-branch" {
			t.Errorf("expected second ref update to be real branch, got %q", updatedRefs[1])
		}
	})

	t.Run("failure on commit does not update real branch", func(t *testing.T) {
		var updatedRealBranch bool

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("initial")},
				})

			case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "heads/test-branch"):
				updatedRealBranch = true
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("sha")},
				})

			case r.Method == http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)

			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		commitCount := 0
		client := newTestClient(t, server)
		client.graphql = &mockGraphQL{
			handler: func(query string, variables map[string]any) (json.RawMessage, error) {
				commitCount++
				if commitCount == 2 {
					return nil, fmt.Errorf("simulated failure")
				}
				return signedGraphQLResponse("commit-sha"), nil
			},
		}

		changes := []Change{
			{hash: "h1", message: "First", entries: map[string]FileEntry{"a.txt": newFileEntry([]byte("a"))}},
			{hash: "h2", message: "Second", entries: map[string]FileEntry{"b.txt": newFileEntry([]byte("b"))}},
		}

		count, _, err := client.PushChanges(context.Background(), "initial", changes...)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if count != 2 {
			t.Errorf("expected count 2 (failed on second), got %d", count)
		}
		if updatedRealBranch {
			t.Error("real branch should not have been updated on failure")
		}
	})

	t.Run("update ref fails", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/refs":
				w.WriteHeader(http.StatusCreated)
				json.NewEncoder(w).Encode(github.Reference{
					Object: &github.GitObject{SHA: github.Ptr("initial")},
				})

			case r.Method == http.MethodPatch:
				w.WriteHeader(http.StatusConflict)

			case r.Method == http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)

			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		client := newTestClient(t, server)
		client.graphql = &mockGraphQL{
			handler: func(query string, variables map[string]any) (json.RawMessage, error) {
				return signedGraphQLResponse("commit-sha"), nil
			},
		}

		changes := []Change{
			{hash: "h1", message: "First", entries: map[string]FileEntry{"a.txt": newFileEntry([]byte("a"))}},
		}

		_, _, err := client.PushChanges(context.Background(), "parent", changes...)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "update ref") {
			t.Errorf("expected 'update ref' error, got %q", err.Error())
		}
	})

	t.Run("dry run skips throwaway branch", func(t *testing.T) {
		client := &Client{
			owner:  "test-owner",
			repo:   "test-repo",
			branch: "test-branch",
			dryrun: true,
		}

		changes := []Change{
			{hash: "h1", message: "First", entries: map[string]FileEntry{"a.txt": newFileEntry([]byte("a"))}},
		}

		count, _, err := client.PushChanges(context.Background(), "initial", changes...)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if count != 1 {
			t.Errorf("expected count 1, got %d", count)
		}
	})
}

func TestRESTBlobUsesBase64Encoding(t *testing.T) {
	// Regression test: binary file content must be base64-encoded when creating
	// blobs via the REST API. Using utf-8 encoding corrupts binary data (e.g. ELF
	// executables) because invalid UTF-8 sequences are mangled during string
	// conversion and JSON marshaling.
	binaryContent := []byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFE, 0x80, 0x90}

	var capturedEncoding, capturedContent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/test-owner/test-repo/git/commits/"):
			json.NewEncoder(w).Encode(github.Commit{
				SHA:  github.Ptr("parent-sha"),
				Tree: &github.Tree{SHA: github.Ptr("parent-tree-sha")},
			})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/blobs":
			var req struct {
				Content  string `json:"content"`
				Encoding string `json:"encoding"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			capturedEncoding = req.Encoding
			capturedContent = req.Content

			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(github.Blob{SHA: github.Ptr("blob-sha")})

		case r.Method == http.MethodPost && r.URL.Path == "/repos/test-owner/test-repo/git/trees":
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(github.Tree{SHA: github.Ptr("tree-sha")})

		default:
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{})
		}
	}))
	defer server.Close()

	client := newTestClient(t, server)
	change := Change{
		entries: map[string]FileEntry{
			"binary-file": {Content: binaryContent, Mode: "100755"},
		},
	}

	_, err := client.prepTree(context.Background(), "parent-sha", change)
	if err != nil {
		t.Fatalf("prepTree failed: %v", err)
	}

	if capturedEncoding != "base64" {
		t.Errorf("expected blob encoding 'base64', got %q", capturedEncoding)
	}

	expectedContent := base64.StdEncoding.EncodeToString(binaryContent)
	if capturedContent != expectedContent {
		t.Errorf("blob content mismatch\n  got:  %q\n  want: %q", capturedContent, expectedContent)
	}

	// Verify round-trip: decoding the sent content must yield the original bytes
	decoded, err := base64.StdEncoding.DecodeString(capturedContent)
	if err != nil {
		t.Fatalf("failed to decode captured content: %v", err)
	}
	if len(decoded) != len(binaryContent) {
		t.Fatalf("decoded length %d != original length %d", len(decoded), len(binaryContent))
	}
	for i := range binaryContent {
		if decoded[i] != binaryContent[i] {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x", i, decoded[i], binaryContent[i])
		}
	}
}

func TestChooseStrategy(t *testing.T) {
	tests := []struct {
		name      string
		userToken bool
		mode      string
		want      commitStrategy
	}{
		{"default mode any token", false, "100644", strategyGraphQL},
		{"empty mode any token", false, "", strategyGraphQL},
		{"executable with user token", true, "100755", strategyGraphQL},
		{"executable with non-user token", false, "100755", strategyREST},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &Client{userToken: tt.userToken}
			change := Change{entries: map[string]FileEntry{"f": {Content: []byte("x"), Mode: tt.mode}}}
			if got := client.chooseStrategy(change); got != tt.want {
				t.Errorf("chooseStrategy() = %v, want %v", got, tt.want)
			}
		})
	}
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
