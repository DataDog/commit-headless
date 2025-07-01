package main

import (
	"testing"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"
	"github.com/google/go-cmp/cmp"
)

func requireNoError(t *testing.T, err error, msg ...any) {
	t.Helper()

	if err == nil {
		return
	}

	if len(msg) == 0 {
		t.Fatalf("expected no error, got: %s", err.Error())
		return
	}

	if len(msg) == 1 {
		t.Fatal(msg[0].(string))
		return
	}

	t.Fatalf(msg[0].(string), msg[1:]...)
}

func TestGitFindChange(t *testing.T) {
	revisions := []string{}

	commitOptions := &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test User",
			Email: "test@test.com",
			When:  time.Now(),
		},
	}

	fs := memfs.New()
	r, err := git.Init(memory.NewStorage(), git.WithWorkTree(fs))
	requireNoError(t, err)

	f, err := fs.Create("README.md")
	requireNoError(t, err)

	f.Write([]byte("Hello, world!"))
	f.Close()

	wt, err := r.Worktree()
	requireNoError(t, err)

	wt.Add("README.md")
	rev, err := wt.Commit("commit message", commitOptions)
	requireNoError(t, err)

	revisions = append(revisions, rev.String())

	// Now, modify and rename README.md -> README.markdown
	f, err = fs.Create("README.markdown")
	requireNoError(t, err)

	f.Write([]byte("Hello, bot!"))
	f.Close()

	// git would (sometimes) track this as a rename, depending on how much the content changed
	// in our case a rename is always presented as a delete on the old file and a write on the new
	// file
	wt.Remove("README.md")
	wt.Add("README.markdown")

	rev, err = wt.Commit("commit message 2\nbody", commitOptions)
	requireNoError(t, err)

	revisions = append(revisions, rev.String())

	repo := &Repository{inner: r}

	changes, err := repo.Changes(revisions...)
	requireNoError(t, err)

	// Quick assertion on the contents of the changes
	want := []Change{
		{
			hash:     revisions[0],
			Headline: "commit message",
			Body:     "Co-authored-by: Test User <test@test.com>",
			Changes: map[string][]byte{
				"README.md": []byte("Hello, world!"),
			},
		},
		{
			hash:     revisions[1],
			Headline: "commit message 2",
			Body:     "body\n\nCo-authored-by: Test User <test@test.com>",
			Changes: map[string][]byte{
				"README.md":       {},
				"README.markdown": []byte("Hello, bot!"),
			},
		},
	}

	if diff := cmp.Diff(want, changes, cmp.AllowUnexported(Change{})); diff != "" {
		t.Errorf("change mismatch (-want, +got):\n%s", diff)
	}
}
