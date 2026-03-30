package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

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
	DeleteRef(ctx context.Context, owner, repo, ref string) (*github.Response, error)
}

// GraphQLAPI abstracts the GraphQL endpoint for testing.
type GraphQLAPI interface {
	Do(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error)
}

// graphQLClient is the production implementation of GraphQLAPI using raw HTTP.
type graphQLClient struct {
	httpClient *http.Client
	endpoint   string
}

func (g *graphQLClient) Do(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	body := map[string]any{
		"query":     query,
		"variables": variables,
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("create graphql request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode graphql response: %w", err)
	}

	if len(result.Errors) > 0 {
		msgs := make([]string, len(result.Errors))
		for i, e := range result.Errors {
			msgs[i] = e.Message
		}
		return nil, fmt.Errorf("graphql errors: %s", strings.Join(msgs, "; "))
	}

	return result.Data, nil
}

// Client provides methods for interacting with a remote repository on GitHub
type Client struct {
	repos   RepositoriesAPI
	git     GitAPI
	graphql GraphQLAPI
	owner   string
	repo    string
	branch  string

	userToken    bool
	dryrun       bool
	force        bool
	signAttempts int
}

// NewClient returns a Client configured to make GitHub requests for branch owned by owner/repo on
// GitHub using the oauth token in token.
func NewClient(ctx context.Context, token, owner, repo, branch string) *Client {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	httpC := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(httpC)

	return &Client{
		repos: ghClient.Repositories,
		git:   ghClient.Git,
		graphql: &graphQLClient{
			httpClient: httpC,
			endpoint:   "https://api.github.com/graphql",
		},
		owner:     owner,
		repo:      repo,
		branch:    branch,
		userToken: isUserToken(token),
	}
}

func (c *Client) commitURL(hash string) string {
	return fmt.Sprintf("https://github.com/%s/%s/commit/%s", c.owner, c.repo, hash)
}

func (c *Client) compareURL(base, head string) string {
	return fmt.Sprintf("https://github.com/%s/%s/compare/%s...%s", c.owner, c.repo, base, head)
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
	logger.Printf("Creating branch from commit %s\n", headSha)

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

// commitStrategy indicates which API to use for creating a commit.
type commitStrategy int

const (
	strategyGraphQL commitStrategy = iota
	strategyREST
)

func (s commitStrategy) String() string {
	if s == strategyGraphQL {
		return "GraphQL"
	}
	return "REST"
}

// chooseStrategy decides whether to use GraphQL or REST for a given change.
func (c *Client) chooseStrategy(change Change) commitStrategy {
	if change.HasNonDefaultModes() && !c.userToken {
		return strategyREST
	}
	return strategyGraphQL
}

// PushChanges creates commits for each change on a throwaway branch, then updates the real branch
// ref once at the end. The throwaway branch is always cleaned up on completion.
// It returns the number of changes that were successfully pushed, the new head reference hash, and
// any error encountered.
func (c *Client) PushChanges(ctx context.Context, headCommit string, changes ...Change) (int, string, error) {
	if c.dryrun {
		for i, change := range changes {
			_, err := c.CreateChange(ctx, "", headCommit, change)
			if err != nil {
				return i + 1, "", fmt.Errorf("push change %d: %w", i, err)
			}
		}
		return len(changes), strings.Repeat("0", 40), nil
	}

	// Create throwaway branch
	tmpBranch := fmt.Sprintf("%s--headless-tmp-%s", c.branch, randomSuffix())
	tmpRef := fmt.Sprintf("refs/heads/%s", tmpBranch)

	logger.Printf("Creating working branch %s\n", tmpBranch)
	_, _, err := c.git.CreateRef(ctx, c.owner, c.repo, github.CreateRef{
		Ref: tmpRef,
		SHA: headCommit,
	})
	if err != nil {
		return 0, "", fmt.Errorf("create working branch: %w", err)
	}

	// Always clean up the throwaway branch
	defer func() {
		logger.Printf("Cleaning up working branch %s\n", tmpBranch)
		if _, err := c.git.DeleteRef(ctx, c.owner, c.repo, tmpRef); err != nil {
			logger.Warningf("Failed to delete working branch %s: %v", tmpBranch, err)
		}
	}()

	// Create commits on the throwaway branch
	for i, change := range changes {
		newHead, err := c.CreateChange(ctx, tmpBranch, headCommit, change)
		if err != nil {
			return i + 1, "", fmt.Errorf("push change %d: %w", i, err)
		}

		strategy := c.chooseStrategy(change)
		if strategy == strategyREST {
			// REST creates a detached commit; advance the throwaway branch to stay in sync
			_, _, err = c.git.UpdateRef(ctx, c.owner, c.repo, tmpRef, github.UpdateRef{
				SHA:   newHead,
				Force: github.Ptr(true),
			})
			if err != nil {
				return i + 1, "", fmt.Errorf("advance working branch: %w", err)
			}
		}

		headCommit = newHead
	}

	// Update the real branch
	_, _, err = c.git.UpdateRef(ctx, c.owner, c.repo, "refs/heads/"+c.branch, github.UpdateRef{
		SHA:   headCommit,
		Force: github.Ptr(c.force),
	})
	if err != nil {
		return len(changes), "", fmt.Errorf("update ref: %w", err)
	}

	return len(changes), headCommit, nil
}

// CreateChange creates a single commit from a change, dispatching to GraphQL or REST based on
// the change contents and token type. It handles logging, dry-run, and signature verification
// retry for both strategies.
//
// tmpBranch is the name of the throwaway branch (without refs/heads/ prefix) used for GraphQL
// commits. It may be empty for dry-run mode.
func (c *Client) CreateChange(ctx context.Context, tmpBranch, headCommit string, change Change) (string, error) {
	shortHash := change.hash
	if len(shortHash) > 8 {
		shortHash = shortHash[:8]
	}
	endGroup := logger.Group(fmt.Sprintf("Commit %s: %s", shortHash, change.Headline()))
	defer endGroup()

	// Log commit details
	if change.author != "" {
		logger.Printf("Author: %s\n", change.author)
	}
	if body := change.Body(); body != "" {
		logger.Printf("Body: %s\n", body)
	}
	logger.Printf("Changed files: %d\n", len(change.entries))
	for path, fe := range change.entries {
		action := "MODIFY"
		if fe.Content == nil {
			action = "DELETE"
		}
		logger.Printf("  - %s: %s\n", action, path)
	}

	strategy := c.chooseStrategy(change)
	logger.Printf("Strategy: %s\n", strategy)

	if strategy == strategyGraphQL && change.HasNonDefaultModes() {
		logger.Warningf("GraphQL API does not preserve file modes; non-default modes in this commit will be reset to 100644")
	}

	if c.dryrun {
		logger.Notice("Dry run enabled, not writing commit")
		return strings.Repeat("0", len(change.hash)), nil
	}

	// Build the commit function based on strategy
	var commitFn func() (string, bool, error)

	switch strategy {
	case strategyGraphQL:
		commitFn = func() (string, bool, error) {
			return c.execGraphQLCommit(ctx, tmpBranch, headCommit, change)
		}
	case strategyREST:
		// Prep tree once before the retry loop
		treeSHA, err := c.prepTree(ctx, headCommit, change)
		if err != nil {
			return "", err
		}
		commitFn = func() (string, bool, error) {
			return c.execRESTCommit(ctx, headCommit, treeSHA, change)
		}
	}

	// Execute with signature verification retry
	commitSha, verified, err := commitFn()
	if err != nil {
		return "", fmt.Errorf("create commit: %w", err)
	}

	if c.signAttempts > 0 {
		backoff := 1 * time.Second
		for attempt := 1; attempt <= c.signAttempts && !verified; attempt++ {
			if attempt == c.signAttempts {
				return "", fmt.Errorf("commit %s was not signed after %d attempt(s)", commitSha, c.signAttempts)
			}

			logger.Warningf("Commit %s not signed (attempt %d/%d), retrying in %s...", commitSha, attempt, c.signAttempts, backoff)
			time.Sleep(backoff)
			backoff *= 2

			commitSha, verified, err = commitFn()
			if err != nil {
				return "", fmt.Errorf("create commit (attempt %d): %w", attempt+1, err)
			}
		}
	}

	logger.Printf("Created: %s\n", c.commitURL(commitSha))
	return commitSha, nil
}

// prepTree creates blobs and a tree for the REST commit path. This is called once before the
// retry loop since the tree is immutable and can be reused across retries.
func (c *Client) prepTree(ctx context.Context, headCommit string, change Change) (string, error) {
	parentCommit, _, err := c.git.GetCommit(ctx, c.owner, c.repo, headCommit)
	if err != nil {
		return "", fmt.Errorf("get parent commit: %w", err)
	}
	baseTreeSHA := parentCommit.GetTree().GetSHA()

	var entries []*github.TreeEntry
	for path, fe := range change.entries {
		mode := fe.Mode
		if mode == "" {
			mode = "100644"
		}

		entry := &github.TreeEntry{
			Path: github.Ptr(path),
			Mode: github.Ptr(mode),
			Type: github.Ptr("blob"),
		}
		if fe.Content == nil {
			// Deletion: SHA must be empty string for go-github to omit it
		} else {
			blob, _, err := c.git.CreateBlob(ctx, c.owner, c.repo, github.Blob{
				Content:  github.Ptr(string(fe.Content)),
				Encoding: github.Ptr("utf-8"),
			})
			if err != nil {
				return "", fmt.Errorf("create blob for %s: %w", path, err)
			}
			entry.SHA = blob.SHA
		}
		entries = append(entries, entry)
	}

	tree, _, err := c.git.CreateTree(ctx, c.owner, c.repo, baseTreeSHA, entries)
	if err != nil {
		return "", fmt.Errorf("create tree: %w", err)
	}

	return tree.GetSHA(), nil
}

// execRESTCommit creates a commit using the REST API with a pre-built tree.
func (c *Client) execRESTCommit(ctx context.Context, headCommit, treeSHA string, change Change) (string, bool, error) {
	message := change.Headline()
	if body := change.Body(); body != "" {
		message = message + "\n\n" + body
	}

	commitInput := github.Commit{
		Message: github.Ptr(message),
		Tree:    &github.Tree{SHA: github.Ptr(treeSHA)},
		Parents: []*github.Commit{{SHA: github.Ptr(headCommit)}},
	}

	commit, _, err := c.git.CreateCommit(ctx, c.owner, c.repo, commitInput, nil)
	if err != nil {
		return "", false, fmt.Errorf("REST create commit: %w", err)
	}

	return commit.GetSHA(), commit.GetVerification().GetVerified(), nil
}

// createCommitOnBranch GraphQL mutation
const createCommitOnBranchMutation = `
mutation CreateCommitOnBranch($input: CreateCommitOnBranchInput!) {
  createCommitOnBranch(input: $input) {
    commit {
      oid
      signature {
        isValid
      }
    }
  }
}
`

// execGraphQLCommit creates a commit using the GraphQL createCommitOnBranch mutation.
func (c *Client) execGraphQLCommit(ctx context.Context, tmpBranch, headCommit string, change Change) (string, bool, error) {
	// Build file additions and deletions
	var additions []map[string]string
	var deletions []map[string]string

	for path, fe := range change.entries {
		if fe.Content == nil {
			deletions = append(deletions, map[string]string{"path": path})
		} else {
			additions = append(additions, map[string]string{
				"path":     path,
				"contents": base64.StdEncoding.EncodeToString(fe.Content),
			})
		}
	}

	headline := change.Headline()
	body := change.Body()

	message := map[string]string{"headline": headline}
	if body != "" {
		message["body"] = body
	}

	fileChanges := map[string]any{}
	if len(additions) > 0 {
		fileChanges["additions"] = additions
	}
	if len(deletions) > 0 {
		fileChanges["deletions"] = deletions
	}

	input := map[string]any{
		"branch": map[string]any{
			"repositoryNameWithOwner": fmt.Sprintf("%s/%s", c.owner, c.repo),
			"branchName":              tmpBranch,
		},
		"expectedHeadOid": headCommit,
		"message":         message,
		"fileChanges":     fileChanges,
	}

	data, err := c.graphql.Do(ctx, createCommitOnBranchMutation, map[string]any{"input": input})
	if err != nil {
		return "", false, fmt.Errorf("GraphQL createCommitOnBranch: %w", err)
	}

	var result struct {
		CreateCommitOnBranch struct {
			Commit struct {
				OID       string `json:"oid"`
				Signature struct {
					IsValid bool `json:"isValid"`
				} `json:"signature"`
			} `json:"commit"`
		} `json:"createCommitOnBranch"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", false, fmt.Errorf("unmarshal GraphQL response: %w", err)
	}

	oid := result.CreateCommitOnBranch.Commit.OID
	signed := result.CreateCommitOnBranch.Commit.Signature.IsValid

	return oid, signed, nil
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.IntN(len(chars))]
	}
	return string(b)
}
