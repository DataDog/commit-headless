package main

import (
	"fmt"
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

	// Isolate from host git config. CI runners commonly inject a
	// url.<token-prefixed-mirror>.insteadOf rewrite of github.com URLs via
	// `git config --global`, which would otherwise leak into tests that
	// operate on remote URLs.
	//
	// HOME is overridden because git versions older than 2.32 don't honor
	// GIT_CONFIG_GLOBAL and fall back to $HOME/.gitconfig — pointing HOME at
	// the empty test tempdir suppresses the global config regardless of git
	// version. GIT_CONFIG_GLOBAL/SYSTEM cover the modern path. These env vars
	// propagate to all git subprocesses (including those inside production
	// code under test) since neither tr.git nor production helpers override
	// cmd.Env.
	tr.t.Setenv("HOME", tr.root)
	tr.t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	tr.t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

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

// diagnoseConfigIsolation returns a string describing whether our test-level
// git config isolation took effect. Used in test failure messages so a CI
// failure tells us whether t.Setenv didn't propagate (env vars empty) or git
// read config from somewhere our env vars don't cover (config sources show
// unexpected paths).
func (tr *testRepository) diagnoseConfigIsolation() string {
	envSnapshot := fmt.Sprintf(
		"  GIT_CONFIG_GLOBAL=%q\n  GIT_CONFIG_SYSTEM=%q\n  HOME=%q\n  XDG_CONFIG_HOME=%q",
		os.Getenv("GIT_CONFIG_GLOBAL"),
		os.Getenv("GIT_CONFIG_SYSTEM"),
		os.Getenv("HOME"),
		os.Getenv("XDG_CONFIG_HOME"),
	)
	cmd := exec.Command("git", "config", "--show-origin", "--list")
	cmd.Dir = tr.root
	out, err := cmd.Output()
	configSources := strings.TrimSpace(string(out))
	if err != nil {
		configSources = fmt.Sprintf("(error running git config --show-origin: %s)", err)
	}
	return fmt.Sprintf("env from test process:\n%s\nconfig sources git is reading:\n%s",
		envSnapshot, configSources)
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

func TestIsClean(t *testing.T) {
	tr := testRepo(t)
	requireNoError(t, os.WriteFile(tr.path("tracked"), []byte("a"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "initial")

	r := &Repository{path: tr.root}

	t.Run("clean", func(t *testing.T) {
		clean, summary, err := r.IsClean()
		requireNoError(t, err)
		if !clean {
			t.Errorf("expected clean, got dirty: %q", summary)
		}
	})

	t.Run("untracked is clean", func(t *testing.T) {
		requireNoError(t, os.WriteFile(tr.path("untracked"), []byte("u"), 0o644))
		defer os.Remove(tr.path("untracked"))

		clean, summary, err := r.IsClean()
		requireNoError(t, err)
		if !clean {
			t.Errorf("expected clean (untracked-only), got dirty: %q", summary)
		}
	})

	t.Run("unstaged modification is dirty", func(t *testing.T) {
		requireNoError(t, os.WriteFile(tr.path("tracked"), []byte("b"), 0o644))
		defer func() {
			tr.git("checkout", "HEAD", "--", "tracked")
		}()

		clean, summary, err := r.IsClean()
		requireNoError(t, err)
		if clean {
			t.Errorf("expected dirty, got clean")
		}
		if !strings.Contains(summary, "tracked") {
			t.Errorf("summary should mention tracked, got: %q", summary)
		}
	})

	t.Run("staged addition is dirty", func(t *testing.T) {
		requireNoError(t, os.WriteFile(tr.path("staged"), []byte("s"), 0o644))
		tr.git("add", "staged")
		defer func() {
			tr.git("reset", "HEAD", "staged")
			os.Remove(tr.path("staged"))
		}()

		clean, _, err := r.IsClean()
		requireNoError(t, err)
		if clean {
			t.Errorf("expected dirty, got clean")
		}
	})
}

func TestCurrentBranch(t *testing.T) {
	tr := testRepo(t)
	tr.git("checkout", "-b", "feature")
	requireNoError(t, os.WriteFile(tr.path("file"), []byte("x"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "first")

	r := &Repository{path: tr.root}

	t.Run("on branch", func(t *testing.T) {
		b, err := r.CurrentBranch()
		requireNoError(t, err)
		if b != "feature" {
			t.Errorf("expected feature, got %q", b)
		}
	})

	t.Run("detached HEAD", func(t *testing.T) {
		head := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))
		tr.git("checkout", "--detach", head)
		defer tr.git("checkout", "feature")

		b, err := r.CurrentBranch()
		requireNoError(t, err)
		if b != "" {
			t.Errorf("expected empty (detached), got %q", b)
		}
	})
}

func TestResetHard(t *testing.T) {
	tr := testRepo(t)
	requireNoError(t, os.WriteFile(tr.path("file"), []byte("v1"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "v1")
	first := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	requireNoError(t, os.WriteFile(tr.path("file"), []byte("v2"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "v2")

	r := &Repository{path: tr.root}

	t.Run("happy path", func(t *testing.T) {
		err := r.ResetHard(first)
		requireNoError(t, err)
		head := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))
		if head != first {
			t.Errorf("expected HEAD %s, got %s", first, head)
		}
		content, err := os.ReadFile(tr.path("file"))
		requireNoError(t, err)
		if string(content) != "v1" {
			t.Errorf("expected file to be v1, got %q", content)
		}
	})

	t.Run("missing ref", func(t *testing.T) {
		err := r.ResetHard("nonexistent-ref-xyz")
		if err == nil {
			t.Error("expected error for unknown ref")
		}
	})
}

func TestRemoteForTarget(t *testing.T) {
	t.Run("matching", func(t *testing.T) {
		cases := []struct {
			url    string
			target string
			match  bool
		}{
			{"https://github.com/DataDog/repo.git", "github.com/datadog/repo", true},
			{"https://github.com/DataDog/repo", "github.com/datadog/repo", true},
			{"git@github.com:DataDog/repo.git", "github.com/datadog/repo", true},
			{"git@github.com:DataDog/repo", "github.com/datadog/repo", true},
			{"git://github.com/DataDog/repo.git", "github.com/datadog/repo", true},
			{"https://github.com/DataDog/other.git", "github.com/datadog/repo", false},
			{"https://gitlab.com/DataDog/repo.git", "github.com/datadog/repo", false},
			{"https://example.com/github.com/datadog/other.git", "github.com/datadog/repo", false},
		}
		for _, tc := range cases {
			t.Run(tc.url, func(t *testing.T) {
				got := remoteURLMatchesTarget(tc.url, tc.target)
				if got != tc.match {
					t.Errorf("remoteURLMatchesTarget(%q, %q) = %v, want %v", tc.url, tc.target, got, tc.match)
				}
			})
		}
	})

	t.Run("single match", func(t *testing.T) {
		tr := testRepo(t)
		tr.git("remote", "add", "origin", "https://github.com/DataDog/example.git")

		r := &Repository{path: tr.root}
		name, err := r.RemoteForTarget("DataDog", "example")
		if err != nil {
			t.Fatalf("expected to find origin, got: %s\n%s", err, tr.diagnoseConfigIsolation())
		}
		if name != "origin" {
			t.Errorf("expected origin, got %q", name)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		tr := testRepo(t)
		tr.git("remote", "add", "origin", "https://github.com/DataDog/Example.git")

		r := &Repository{path: tr.root}
		name, err := r.RemoteForTarget("datadog", "example")
		if err != nil {
			t.Fatalf("expected to find origin, got: %s\n%s", err, tr.diagnoseConfigIsolation())
		}
		if name != "origin" {
			t.Errorf("expected origin, got %q", name)
		}
	})

	t.Run("ssh url", func(t *testing.T) {
		tr := testRepo(t)
		tr.git("remote", "add", "upstream", "git@github.com:DataDog/example.git")

		r := &Repository{path: tr.root}
		name, err := r.RemoteForTarget("DataDog", "example")
		requireNoError(t, err)
		if name != "upstream" {
			t.Errorf("expected upstream, got %q", name)
		}
	})

	t.Run("no match", func(t *testing.T) {
		tr := testRepo(t)
		tr.git("remote", "add", "origin", "https://github.com/DataDog/other.git")

		r := &Repository{path: tr.root}
		_, err := r.RemoteForTarget("DataDog", "example")
		if err == nil {
			t.Error("expected error when no remote matches")
		}
	})

	t.Run("multiple matches", func(t *testing.T) {
		tr := testRepo(t)
		tr.git("remote", "add", "origin", "https://github.com/DataDog/example.git")
		tr.git("remote", "add", "fork", "git@github.com:DataDog/example.git")

		r := &Repository{path: tr.root}
		_, err := r.RemoteForTarget("DataDog", "example")
		if err == nil {
			t.Fatalf("expected error when multiple remotes match\n%s", tr.diagnoseConfigIsolation())
		}
		if !strings.Contains(err.Error(), "multiple") {
			t.Errorf("expected 'multiple' in error, got: %v", err)
		}
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

func TestChangedFilesSubmodule(t *testing.T) {
	sub := testRepo(t)
	requireNoError(t, os.WriteFile(sub.path("file"), []byte("content"), 0o644))
	sub.git("add", "-A")
	sub.git("commit", "--message", "submodule initial commit")
	subHash := strings.TrimSpace(string(sub.git("rev-parse", "HEAD")))

	tr := testRepo(t)
	requireNoError(t, os.WriteFile(tr.path("file"), []byte("content"), 0o644))
	tr.git("add", "-A")
	tr.git("commit", "--message", "initial commit")

	tr.git("-c", "protocol.file.allow=always", "submodule", "add", sub.root, "submodules/sub")
	tr.git("commit", "--message", "add submodule")

	runIn := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"HOME="+tr.root,
			"GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_CONFIG_SYSTEM="+os.DevNull,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	subCheckout := tr.path("submodules/sub")
	runIn(subCheckout, "config", "user.name", "A U Thor")
	runIn(subCheckout, "config", "user.email", "author@home.arpa")

	// Bump the pinned commit to simulate an SDK pin update.
	requireNoError(t, os.WriteFile(sub.path("file"), []byte("more content"), 0o644))
	sub.git("commit", "-a", "--message", "submodule second commit")
	newSubHash := strings.TrimSpace(string(sub.git("rev-parse", "HEAD")))

	runIn(subCheckout, "fetch")
	runIn(subCheckout, "checkout", newSubHash)

	tr.git("add", "-A")
	tr.git("commit", "--message", "bump submodule pin")
	hash := strings.TrimSpace(string(tr.git("rev-parse", "HEAD")))

	r := &Repository{path: tr.root}

	changes, err := r.Changes(hash)
	requireNoError(t, err, "expected the submodule pin bump to be readable without a cat-file error")

	entry, ok := changes[0].entries["submodules/sub"]
	if !ok {
		t.Fatalf("expected an entry for submodules/sub, got %v", changes[0].entries)
	}
	if entry.Mode != modeSubmodule {
		t.Fatalf("expected mode %q, got %q", modeSubmodule, entry.Mode)
	}
	if !entry.IsSubmodule() {
		t.Fatal("expected IsSubmodule() to be true")
	}
	if string(entry.Content) != newSubHash {
		t.Fatalf("expected content to be the submodule's new commit hash %q, got %q", newSubHash, entry.Content)
	}

	_ = subHash // establishes the initial pin; unused beyond documenting the setup
}
