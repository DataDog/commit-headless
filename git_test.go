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

	if change.entries["to-empty"] == nil {
		t.Log("expected to-empty to be empty, not nil")
		t.Fail()
	}

	if change.entries["to-delete"] != nil {
		t.Logf("expected to-delete to be nil, got %q", change.entries["to-delete"])
		t.Fail()
	}
}
