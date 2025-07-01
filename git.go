package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/utils/merkletrie"
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
			return nil, fmt.Errorf("resolve revision %s: %w", c, err)
		}

		change, err := r.changed(*h)
		if err != nil {
			return nil, fmt.Errorf("get changes (%s): %w", *h, err)
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// changed returns a github.Change for the provided commit hash
func (r *Repository) changed(h plumbing.Hash) (Change, error) {
	c, err := r.inner.CommitObject(h)
	if err != nil {
		return Change{}, fmt.Errorf("read commit %s: %w", h, err)
	}

	if c.NumParents() > 1 {
		// TODO: Consider if we want to support merge commits at all. There's a lot of risk of
		// reproducing a crap ton of commits from, eg, main, which shouldn't really be a factor
		// in this workflow. That is, unless a use case requires signed merge commits, we're
		// going to leave this in place.
		return Change{}, fmt.Errorf("range includes a merge commit (%s), not continuing", h)
	}

	headline, body, _ := strings.Cut(strings.TrimSpace(c.Message), "\n")

	if body != "" {
		body += "\n\n"
	}

	body = fmt.Sprintf("%sCo-authored-by: %s <%s>", body, c.Author.Name, c.Author.Email)

	input := Change{
		hash:     h.String(),
		Headline: headline,
		Body:     body,
		Changes:  map[string][]byte{},
	}

	tree, err := c.Tree()
	if err != nil {
		return input, fmt.Errorf("get tree %s: %w", h, err)
	}

	var ptree *object.Tree
	if c.NumParents() == 1 {
		parent, err := c.Parent(0)
		if err != nil {
			return input, fmt.Errorf("get parent %s: %w", h, err)
		}

		ptree, err = parent.Tree()
		if err != nil {
			return input, fmt.Errorf("get parent tree %s: %w", h, err)
		}
	} else {
		ptree = &object.Tree{}
	}

	diff, err := ptree.Diff(tree)
	if err != nil {
		return input, fmt.Errorf("compute diff %s: %w", h, err)
	}

	for _, change := range diff {
		action, err := change.Action()
		if err != nil {
			return input, fmt.Errorf("get change type %s: %w", h, err)
		}

		if action == merkletrie.Delete {
			input.Changes[change.From.Name] = []byte{}
			continue
		}

		// This is a rename, which is a delete on the old name and a modify on the new name
		// so we mark the from as a delete but continue execution
		if change.From.Name != "" && change.From.Name != change.To.Name {
			input.Changes[change.From.Name] = []byte{}
		}

		name := change.To.Name

		content, err := fileContents(tree, name)
		if err != nil {
			return input, err
		}

		input.Changes[name] = content
	}

	return input, nil
}

// fileContents takes an object.Tree and filename and returns the contents of the file, if any
func fileContents(t *object.Tree, name string) ([]byte, error) {
	f, err := t.File(name)
	// XXX: This shouldn't happen, as it implies the file was deleted but we would have caught
	// that in the action block above.
	if errors.Is(err, object.ErrFileNotFound) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("get file %s: %w", name, err)
	}

	// contents are in f.Contents, or f.Reader
	// since we transmit as bytes and the json encoder already base64 encodes byte strings,
	// we don't need to encode ourselves
	r, err := f.Reader()
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", name, err)
	}

	content, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read file content %s: %w", name, err)
	}

	r.Close()

	return content, nil
}
