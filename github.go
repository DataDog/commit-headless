package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Change represents a change in a given commit.
type Change struct {
	// Message is the commit message
	Message string

	// Changes is a map of path to file contents.
	// Deleted files will map to nil contents or an empty byte slice.
	Changes map[string][]byte
}

// Client provides methods for interacting with a remote repository on GitHub
type Client struct {
	httpC  *http.Client
	owner  string
	repo   string
	branch string

	// Used for testing purposes
	baseURL string
}

// NewClient returns a Client configured to make GitHub requests for branch owned by owner/repo on
// GitHub using the oauth token in token.
func NewClient(ctx context.Context, token, owner, repo, branch string) *Client {
	tokensrc := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)

	httpC := oauth2.NewClient(ctx, tokensrc)
	return &Client{
		httpC: httpC,
		owner: owner, repo: repo, branch: branch,
		baseURL: "https://api.github.com",
	}
}

func (c *Client) branchURL() string {
	return fmt.Sprintf("%s/repos/%s/%s/branches/%s", c.baseURL, c.owner, c.repo, c.branch)
}

func (c *Client) graphqlURL() string {
	return fmt.Sprintf("%s/graphql", c.baseURL)
}

// GetHeadCommitHash returns the current head commit hash for the configured repository and branch
func (c *Client) GetHeadCommitHash(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.branchURL(), nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpC.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get commit hash: http %d", resp.StatusCode)
	}

	payload := struct {
		Commit struct {
			Sha string
		}
	}{}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	return payload.Commit.Sha, nil
}

// PushChanges takes a list of changes and a commit hash and produces commits using the GitHub GraphQL API.
// The commit hash is expected to be the current head of the remote branch, see [GetHeadCommitHash]
// for more.
// It returns the number of changes that were successfully pushed and any error encountered.
func (c *Client) PushChanges(ctx context.Context, headCommit string, changes ...Change) (int, error) {
	var err error
	for i, change := range changes {
		headCommit, err = c.PushChange(ctx, headCommit, change)
		if err != nil {
			return i, err
		}
		fmt.Printf("pushed %d of %d commits, new head: %s\n", i+1, len(changes), headCommit)
	}

	return len(changes), nil
}

// PushChange pushes a single change using the GraphQL API.
// It returns the hash of the pushed commit or an error.
func (c *Client) PushChange(ctx context.Context, headCommit string, change Change) (string, error) {
	// Turn the change into a createCommitOnBranchInput
	headline, body, _ := strings.Cut(change.Message, "\n")

	added := []fileChange{}
	deleted := []fileChange{}

	for path, content := range change.Changes {
		if len(content) == 0 {
			deleted = append(deleted, fileChange{
				Path: path,
			})
		} else {
			added = append(added, fileChange{
				Path:     path,
				Contents: content,
			})
		}
	}

	input := createCommitOnBranchInput{
		Branch: commitInputBranch{
			Name:   c.branch,
			Target: fmt.Sprintf("%s/%s", c.owner, c.repo),
		},
		ExpectedRef: headCommit,
		Message: commitInputMessage{
			Headline: headline,
			Body:     body,
		},
		Changes: commitInputChanges{
			Additions: added,
			Deletions: deleted,
		},
	}

	query := wrapper{
		Query: `
			mutation ($input: CreateCommitOnBranchInput!) {
				createCommitOnBranch(input: $input) {
					commit {
						oid
					}
				}
			}
		`,
		Variables: map[string]any{"input": input},
	}

	// Encode the query to JSON (so we can print it in case of an error)
	queryJSON, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("marshal mutation: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.graphqlURL(), bytes.NewReader(queryJSON))
	if err != nil {
		return "", err
	}

	resp, err := c.httpC.Do(req)
	defer resp.Body.Close()
	if err != nil {
		return "", err
	}

	payload := struct {
		Data struct {
			CreateCommitOnBranch struct {
				Commit struct {
					ObjectID string `json:"oid"`
				}
			} `json:"createCommitOnBranch"`
		}
		Errors []struct {
			Message string
		}
	}{}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode response body: %w", err)
	}

	if len(payload.Errors) != 0 {
		fmt.Printf("There were %d errors returned when creating the commit.\n", len(payload.Errors))
		fmt.Println("Input:")
		fmt.Println(string(queryJSON))
		for i, e := range payload.Errors {
			fmt.Printf("Error %d: %s\n", i+1, e.Message)
		}
		return "", errors.New("graphql error")
	}

	return payload.Data.CreateCommitOnBranch.Commit.ObjectID, nil
}

type wrapper struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type createCommitOnBranchInput struct {
	Branch      commitInputBranch  `json:"branch"`
	ExpectedRef string             `json:"expectedHeadOid"`
	Message     commitInputMessage `json:"message"`
	Changes     commitInputChanges `json:"fileChanges"`
}

type commitInputBranch struct {
	Name   string `json:"branchName"`
	Target string `json:"repositoryNameWithOwner"`
}

type commitInputMessage struct {
	Headline string `json:"headline"`
	Body     string `json:"body"`
}

type commitInputChanges struct {
	Additions []fileChange `json:"additions,omitempty"`
	Deletions []fileChange `json:"deletions,omitempty"`
}

type fileChange struct {
	Path     string `json:"path"`
	Contents []byte `json:"contents,omitempty"`
}
