package main

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func requireNoError(t *testing.T, err error, msg ...any) {
	t.Helper()

	if err == nil {
		return
	}

	if len(msg) == 1 {
		t.Log(msg[0].(string))
	} else if len(msg) > 1 {
		t.Logf(msg[0].(string), msg[1:]...)
	}

	if ee, ok := err.(*exec.ExitError); ok {
		t.Log("STDERR:", string(ee.Stderr))
	}

	t.Fatalf("expected no error, got: %s", err.Error())
}

type testRepository struct {
	t    *testing.T
	root string
}

func (tr *testRepository) init() {
	tr.root = tr.t.TempDir()
	tr.git("init")
	tr.git("config", "user.name", "A U Thor")
	tr.git("config", "user.email", "author@home.arpa")
}

func (tr *testRepository) git(args ...string) []byte {
	cmd := exec.Command("git", args...)
	cmd.Dir = tr.root
	out, err := cmd.Output()
	requireNoError(tr.t, err)
	return out
}

func (tr *testRepository) path(p ...string) string {
	return filepath.Join(append([]string{tr.root}, p...)...)
}

func testRepo(t *testing.T) *testRepository {
	t.Helper()

	tr := &testRepository{t: t}
	tr.init()
	return tr
}

func TestCommitHashes(t *testing.T) {
	testcases := []struct {
		input string
		ok    bool
	}{{
		"deadbeef", true,
	}, {
		"HEAD", false,
	}, {
		"f8034fe40034a602c232b8cbe06ab79e518f71c1", true,
	}, {
		"fee", false,
	}, {
		"f", false,
	}, {
		"", false,
	}, {
		strings.Repeat("a", 40), true,
	}, {
		strings.Repeat("a", 41), false,
	}}

	for _, tc := range testcases {
		t.Run(tc.input, func(t *testing.T) {
			want := tc.ok
			got := hashRegex.MatchString(tc.input)
			if want != got {
				t.Fatalf("commit hash check mismatch; got=%t, want=%t", got, want)
			}
		})
	}
}

func TestCommitsSince(t *testing.T) {
	tr := testRepo(t)

	// Create a few commits
	requireNoError(t, os.WriteFile(tr.path("file1"), []byte("content1"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "first commit")
	hash1 := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	requireNoError(t, os.WriteFile(tr.path("file2"), []byte("content2"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "second commit")
	hash2 := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	requireNoError(t, os.WriteFile(tr.path("file3"), []byte("content3"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "third commit")
	hash3 := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	r := &Repository{path: tr.root}

	t.Run("commits since first", func(t *testing.T) {
		commits, err := r.CommitsSince(hash1)
		requireNoError(t, err)
		if len(commits) != 2 {
			t.Fatalf("expected 2 commits, got %d: %v", len(commits), commits)
		}
		if commits[0] != hash2 || commits[1] != hash3 {
			t.Errorf("expected [%s, %s], got %v", hash2, hash3, commits)
		}
	})

	t.Run("commits since second", func(t *testing.T) {
		commits, err := r.CommitsSince(hash2)
		requireNoError(t, err)
		if len(commits) != 1 {
			t.Fatalf("expected 1 commit, got %d: %v", len(commits), commits)
		}
		if commits[0] != hash3 {
			t.Errorf("expected [%s], got %v", hash3, commits)
		}
	})

	t.Run("commits since HEAD (none)", func(t *testing.T) {
		commits, err := r.CommitsSince(hash3)
		requireNoError(t, err)
		if len(commits) != 0 {
			t.Errorf("expected no commits, got %v", commits)
		}
	})

	t.Run("invalid base", func(t *testing.T) {
		_, err := r.CommitsSince("nonexistent-ref-12345")
		if err == nil {
			t.Error("expected error for invalid reference")
		}
	})

	t.Run("diverged history", func(t *testing.T) {
		// Create a separate branch with different history
		tr.git("checkout", "-b", "other-branch", hash1)
		requireNoError(t, os.WriteFile(tr.path("other-file"), []byte("other"), 0o644))
		tr.git("add", "-A")
		tr.git("commit", "--message", "commit on other branch")
		otherHash := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

		// Go back to main branch
		tr.git("checkout", "-")

		// otherHash is not an ancestor of HEAD (hash3)
		_, err := r.CommitsSince(otherHash)
		if err == nil {
			t.Error("expected error for diverged history")
		}
		if !strings.Contains(err.Error(), "not an ancestor") {
			t.Errorf("expected 'not an ancestor' error, got: %v", err)
		}
	})
}

func TestCommitsBetween(t *testing.T) {
	tr := testRepo(t)

	// Create a few commits
	requireNoError(t, os.WriteFile(tr.path("file1"), []byte("content1"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "first commit")
	hash1 := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	requireNoError(t, os.WriteFile(tr.path("file2"), []byte("content2"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "second commit")
	hash2 := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	requireNoError(t, os.WriteFile(tr.path("file3"), []byte("content3"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "third commit")
	hash3 := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	r := &Repository{path: tr.root}

	t.Run("commits between first and third", func(t *testing.T) {
		commits, err := r.CommitsBetween(hash1, hash3)
		requireNoError(t, err)
		if len(commits) != 2 {
			t.Fatalf("expected 2 commits, got %d: %v", len(commits), commits)
		}
		if commits[0] != hash2 || commits[1] != hash3 {
			t.Errorf("expected [%s, %s], got %v", hash2, hash3, commits)
		}
	})

	t.Run("commits between first and second", func(t *testing.T) {
		commits, err := r.CommitsBetween(hash1, hash2)
		requireNoError(t, err)
		if len(commits) != 1 {
			t.Fatalf("expected 1 commit, got %d: %v", len(commits), commits)
		}
		if commits[0] != hash2 {
			t.Errorf("expected [%s], got %v", hash2, commits)
		}
	})

	t.Run("commits between same commit (none)", func(t *testing.T) {
		commits, err := r.CommitsBetween(hash3, hash3)
		requireNoError(t, err)
		if len(commits) != 0 {
			t.Errorf("expected no commits, got %v", commits)
		}
	})

	t.Run("diverged history", func(t *testing.T) {
		// Create a separate branch with different history
		tr.git("checkout", "-b", "other-branch", hash1)
		requireNoError(t, os.WriteFile(tr.path("other-file"), []byte("other"), 0o644))
		tr.git("add", "-A")
		tr.git("commit", "--message", "commit on other branch")
		otherHash := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

		// Go back to main branch
		tr.git("checkout", "-")

		// otherHash is not an ancestor of hash3
		_, err := r.CommitsBetween(otherHash, hash3)
		if err == nil {
			t.Error("expected error for diverged history")
		}
		if !strings.Contains(err.Error(), "not an ancestor") {
			t.Errorf("expected 'not an ancestor' error, got: %v", err)
		}
	})
}

func TestStagedChanges(t *testing.T) {
	tr := testRepo(t)

	// Create initial commit
	requireNoError(t, os.WriteFile(tr.path("existing.txt"), []byte("original"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "initial")

	r := &Repository{path: tr.root}

	t.Run("no staged changes", func(t *testing.T) {
		changes, err := r.StagedChanges()
		requireNoError(t, err)
		if len(changes) != 0 {
			t.Errorf("expected empty changes, got %d", len(changes))
		}
	})

	t.Run("staged addition", func(t *testing.T) {
		requireNoError(t, os.WriteFile(tr.path("new.txt"), []byte("new content"), 0o644))
		tr.git("add", "new.txt")

		changes, err := r.StagedChanges()
		requireNoError(t, err)

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		if string(changes["new.txt"].Content) != "new content" {
			t.Errorf("unexpected content: %q", changes["new.txt"].Content)
		}
		if changes["new.txt"].Mode != "100644" {
			t.Errorf("unexpected mode: %q", changes["new.txt"].Mode)
		}

		// Cleanup
		tr.git("reset", "HEAD", "new.txt")
		os.Remove(tr.path("new.txt"))
	})

	t.Run("staged executable", func(t *testing.T) {
		requireNoError(t, os.WriteFile(tr.path("script.sh"), []byte("#!/bin/bash\necho hello"), 0o755))
		tr.git("add", "script.sh")

		changes, err := r.StagedChanges()
		requireNoError(t, err)

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		if changes["script.sh"].Mode != "100755" {
			t.Errorf("expected executable mode 100755, got %q", changes["script.sh"].Mode)
		}

		// Cleanup
		tr.git("reset", "HEAD", "script.sh")
		os.Remove(tr.path("script.sh"))
	})

	t.Run("staged modification", func(t *testing.T) {
		requireNoError(t, os.WriteFile(tr.path("existing.txt"), []byte("modified"), 0o644))
		tr.git("add", "existing.txt")

		changes, err := r.StagedChanges()
		requireNoError(t, err)

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		if string(changes["existing.txt"].Content) != "modified" {
			t.Errorf("unexpected content: %q", changes["existing.txt"].Content)
		}

		// Cleanup - restore file to original state
		tr.git("checkout", "HEAD", "--", "existing.txt")
	})

	t.Run("staged deletion", func(t *testing.T) {
		tr.git("rm", "-f", "existing.txt")

		changes, err := r.StagedChanges()
		requireNoError(t, err)

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}
		if changes["existing.txt"].Content != nil {
			t.Errorf("expected nil for deletion, got %q", changes["existing.txt"].Content)
		}

		// Cleanup - restore file
		tr.git("reset", "HEAD", "existing.txt")
		tr.git("checkout", "existing.txt")
	})
}

func TestChangedFiles(t *testing.T) {
	// First, prep the test repository
	tr := testRepo(t)

	requireNoError(t, os.WriteFile(tr.path("file"), []byte("content"), 0o644))
	requireNoError(t, os.WriteFile(tr.path("to-empty"), []byte("content"), 0o644))
	requireNoError(t, os.WriteFile(tr.path("to-delete"), []byte("content"), 0o644))

	tr.git("add", "-A")
	tr.git("commit", "--message", "initial commit")

	requireNoError(t, os.Truncate(tr.path("to-empty"), 0))
	requireNoError(t, os.Remove(tr.path("to-delete")))

	tr.git("add", "-A")
	tr.git("commit", "--message", "second commit")
	hash := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	r := &Repository{path: tr.root}

	changes, err := r.Changes(hash)
	requireNoError(t, err)

	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]

	if len(change.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(change.entries))
	}

	keys := slices.Sorted(maps.Keys(change.entries))
	if keys[0] != "to-delete" || keys[1] != "to-empty" {
		t.Fatalf("expected changed files to be 'to-delete' and 'to-empty', got %q", keys)
	}

	if change.entries["to-empty"].Content == nil {
		t.Log("expected to-empty to have empty content, not nil")
		t.Fail()
	}

	if change.entries["to-delete"].Content != nil {
		t.Logf("expected to-delete to have nil content, got %q", change.entries["to-delete"].Content)
		t.Fail()
	}
}
