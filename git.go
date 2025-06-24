package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
)

type Repository struct {
	inner *git.Repository
}

// Open opens and returns a Repository in the current working directory
func Open(path string) (*Repository, error) {
	r, err := git.PlainOpen(path)
	return &Repository{inner: r}, err
}

// Changes takes a number of commit hashes and returns github.Change records for each commit
func (r *Repository) Changes(commits ...string) ([]Change, error) {
	changes := []Change{}
	for _, c := range commits {
		h, err := r.inner.ResolveRevision(plumbing.Revision(c))
		if err != nil {
			return nil, err
		}

		change, err := r.changed(*h)
		if err != nil {
			return nil, err
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// changed returns a github.Change for the provided commit hash
func (r *Repository) changed(h plumbing.Hash) (Change, error) {
	c, err := r.inner.CommitObject(h)
	if err != nil {
		return Change{}, err
	}

	if c.NumParents() > 1 {
		// TODO: Consider if we want to support merge commits at all. There's a lot of risk of
		// reproducing a crap ton of commits from, eg, main, which shouldn't really be a factor
		// in this workflow. That is, unless a use case requires signed merge commits, we're
		// going to leave this in place.
		return Change{}, fmt.Errorf("range includes a merge commit (%s), not continuing", h)
	}

	fmt.Printf("%s %s\n", c.Hash, strings.Split(c.Message, "\n")[0])

	message := fmt.Sprintf("%s\nCo-authored-by: %s <%s>", c.Message, c.Author.Name, c.Author.Email)

	fmt.Println(message)

	input := Change{
		Message: message,
		Changes: map[string][]byte{},
	}

	tree, err := c.Tree()
	if err != nil {
		return input, fmt.Errorf("commit.tree(%s): %w", h, err)
	}

	parent, err := c.Parent(0)
	if err != nil {
		return input, fmt.Errorf("commit.parent(%s): %w", h, err)
	}

	ptree, err := parent.Tree()
	if err != nil {
		return input, fmt.Errorf("commit.parent.tree(%s): %w", h, err)
	}

	diff, err := ptree.Diff(tree)
	if err != nil {
		return input, fmt.Errorf("tree.diff(%s): %w", h, err)
	}

	fmt.Printf("  changed files: %d\n", len(diff))

	// Each change in the diff represents one file. We don't really care if it modified,
	// created, or removed a file, we just need the set of names so we can get the contents
	// of the file in the current tree. If there are no contents, it's a deletion.
	for _, change := range diff {
		deleted := false
		name := change.To.Name
		if name == "" {
			name = change.From.Name
		}

		f, err := tree.File(name)
		if errors.Is(err, object.ErrFileNotFound) {
			deleted = true
		} else if err != nil {
			return input, fmt.Errorf("tree.file(%s -> %s): %w", h, name, err)
		}

		// contents are in f.Contents, or f.Reader
		// since we transmit as bytes and the json encoder already base64 encodes byte strings,
		// we don't need to encode ourselves
		r, err := f.Reader()
		if err != nil {
			return input, fmt.Errorf("file.reader(%s -> %s): %w", h, name, err)
		}

		content, err := io.ReadAll(r)
		if err != nil {
			return input, fmt.Errorf("file.reader.read(%s -> %s): %w", h, name, err)
		}

		r.Close()

		input.Changes[name] = content

		fmt.Printf("  %s deleted=%t size=%d\n", name, deleted, f.Size)
	}

	return input, nil
}
