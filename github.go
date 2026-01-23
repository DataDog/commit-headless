package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/go-github/v81/github"
	"golang.org/x/oauth2"
)

var ErrNoRemoteBranch = errors.New("branch does not exist on the remote")

// RepositoriesAPI defines the subset of github.RepositoriesService methods needed by this project.
type RepositoriesAPI interface {
	GetBranch(ctx context.Context, owner, repo, branch string, maxRedirects int) (*github.Branch, *github.Response, error)
}

// GitAPI defines the subset of github.GitService methods needed by this project.
type GitAPI interface {
	CreateRef(ctx context.Context, owner, repo string, ref github.CreateRef) (*github.Reference, *github.Response, error)
	GetCommit(ctx context.Context, owner, repo, sha string) (*github.Commit, *github.Response, error)
	CreateBlob(ctx context.Context, owner, repo string, blob github.Blob) (*github.Blob, *github.Response, error)
	CreateTree(ctx context.Context, owner, repo, baseTree string, entries []*github.TreeEntry) (*github.Tree, *github.Response, error)
	CreateCommit(ctx context.Context, owner, repo string, commit github.Commit, opts *github.CreateCommitOptions) (*github.Commit, *github.Response, error)
	UpdateRef(ctx context.Context, owner, repo, ref string, updateRef github.UpdateRef) (*github.Reference, *github.Response, error)
}

// Client provides methods for interacting with a remote repository on GitHub
type Client struct {
	repos  RepositoriesAPI
	git    GitAPI
	owner  string
	repo   string
	branch string

	dryrun bool
}

// NewClient returns a Client configured to make GitHub requests for branch owned by owner/repo on
// GitHub using the oauth token in token.
func NewClient(ctx context.Context, token, owner, repo, branch string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpC := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(httpC)

	return &Client{
		repos:  ghClient.Repositories,
		git:    ghClient.Git,
		owner:  owner,
		repo:   repo,
		branch: branch,
	}
}

func (c *Client) browseCommitsURL() string {
	return fmt.Sprintf("https://github.com/%s/%s/commits/%s", c.owner, c.repo, c.branch)
}

func (c *Client) commitURL(hash string) string {
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", c.owner, c.repo, hash)
}

// GetHeadCommitHash returns the current head commit hash for the configured repository and branch
func (c *Client) GetHeadCommitHash(ctx context.Context) (string, error) {
	branch, resp, err := c.repos.GetBranch(ctx, c.owner, c.repo, c.branch, 0)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return "", fmt.Errorf("get branch %q: %w", c.branch, ErrNoRemoteBranch)
		}
		return "", fmt.Errorf("get commit hash: %w", err)
	}
	return branch.GetCommit().GetSHA(), nil
}

// CreateBranch attempts to create c.branch using headSha as the branch point
func (c *Client) CreateBranch(ctx context.Context, headSha string) (string, error) {
	log("Creating branch from commit %s\n", headSha)

	ref := github.CreateRef{
		Ref: fmt.Sprintf("refs/heads/%s", c.branch),
		SHA: headSha,
	}

	created, resp, err := c.git.CreateRef(ctx, c.owner, c.repo, ref)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity {
			return "", fmt.Errorf("create branch: http 422 (does the branch point exist?)")
		}
		return "", fmt.Errorf("create branch: %w", err)
	}
	return created.GetObject().GetSHA(), nil
}

// PushChanges takes a list of changes and a commit hash and produces commits using the GitHub REST API.
// The commit hash is expected to be the current head of the remote branch, see [GetHeadCommitHash]
// for more.
// It returns the number of changes that were successfully pushed, the new head reference hash, and
// any error encountered.
func (c *Client) PushChanges(ctx context.Context, headCommit string, changes ...Change) (int, string, error) {
	var err error
	for i, change := range changes {
		headCommit, err = c.PushChange(ctx, headCommit, change)
		if err != nil {
			return i + 1, "", fmt.Errorf("push change %d: %w", i+i, err)
		}
	}

	return len(changes), headCommit, nil
}

// PushChange pushes a single change using the REST API.
// It returns the hash of the pushed commit or an error.
func (c *Client) PushChange(ctx context.Context, headCommit string, change Change) (string, error) {
	if c.dryrun {
		log("Dry run enabled, not writing commit.\n")
		return strings.Repeat("0", len(change.hash)), nil
	}

	// Get the parent commit's tree SHA
	parentCommit, _, err := c.git.GetCommit(ctx, c.owner, c.repo, headCommit)
	if err != nil {
		return "", fmt.Errorf("get parent commit: %w", err)
	}
	baseTreeSHA := parentCommit.GetTree().GetSHA()

	// Build tree entries
	var entries []*github.TreeEntry
	for path, content := range change.entries {
		entry := &github.TreeEntry{
			Path: github.Ptr(path),
			Mode: github.Ptr("100644"),
			Type: github.Ptr("blob"),
		}
		if content == nil {
			// Deletion: SHA must be empty string for go-github to omit it
		} else {
			// Create blob for additions/modifications
			blob, _, err := c.git.CreateBlob(ctx, c.owner, c.repo, github.Blob{
				Content:  github.Ptr(string(content)),
				Encoding: github.Ptr("utf-8"),
			})
			if err != nil {
				return "", fmt.Errorf("create blob for %s: %w", path, err)
			}
			entry.SHA = blob.SHA
		}
		entries = append(entries, entry)
	}

	// Create tree
	tree, _, err := c.git.CreateTree(ctx, c.owner, c.repo, baseTreeSHA, entries)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	// Create commit
	message := change.Headline()
	if body := change.Body(); body != "" {
		message = message + "\n\n" + body
	}

	commit, _, err := c.git.CreateCommit(ctx, c.owner, c.repo, github.Commit{
		Message: github.Ptr(message),
		Tree:    &github.Tree{SHA: tree.SHA},
		Parents: []*github.Commit{{SHA: github.Ptr(headCommit)}},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	// Update ref
	_, _, err = c.git.UpdateRef(ctx, c.owner, c.repo, "refs/heads/"+c.branch, github.UpdateRef{
		SHA:   commit.GetSHA(),
		Force: github.Ptr(false),
	})
	if err != nil {
		return "", fmt.Errorf("update ref: %w", err)
	}

	commitSha := commit.GetSHA()
	log("Pushed commit %s -> %s\n", change.hash, commitSha)
	log("  Commit URL: %s\n", c.commitURL(commitSha))

	return commitSha, nil
}
