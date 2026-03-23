package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type ReplayCmd struct {
	baseFlags
	Since string `required:"" help:"Base commit to replay from (exclusive). Commits after this will be replayed."`
}

func (c *ReplayCmd) Help() string {
	return `
This command replays existing remote commits as signed commits. It fetches the remote branch,
extracts commits since the specified base, and recreates them as signed commits using the GitHub
API. The branch ref is then force-updated to point to the new signed commits.

This is useful when you have unsigned commits on a branch (e.g., from a bot or action that doesn't
support signed commits) and want to replace them with signed versions.

You must provide a GitHub token via the environment in one of the following variables, in preference
order:

	- HEADLESS_TOKEN
	- GITHUB_TOKEN
	- GH_TOKEN

Example usage:

	# Replay all commits since abc123 as signed commits
	commit-headless replay -T owner/repo --branch feature --since abc123

	# With safety check that remote HEAD matches expected value
	commit-headless replay -T owner/repo --branch feature --since abc123 --head-sha def456

The --since commit must be an ancestor of the branch HEAD. The commits between --since and HEAD
will be replayed as signed commits, and the branch will be force-updated to point to the new HEAD.

WARNING: This command force-pushes to the remote branch. Use with caution.
`
}

func (c *ReplayCmd) Run() error {
	ctx := context.Background()
	repo := &Repository{path: c.RepoPath}

	token := getToken(os.Getenv)
	if token == "" {
		return fmt.Errorf("no GitHub token supplied")
	}

	client := NewClient(ctx, token, c.Target.Owner(), c.Target.Repository(), c.Branch)
	client.dryrun = c.DryRun
	client.force = true

	// Validate remote HEAD against --head-sha if provided
	if _, err := c.ValidateRemoteHead(ctx, client); err != nil {
		return err
	}

	// Fetch the remote branch
	logger.Printf("Fetching origin/%s...\n", c.Branch)
	if err := repo.Fetch(c.Branch); err != nil {
		return err
	}

	// Get commits between --since and remote HEAD
	remoteRef := fmt.Sprintf("origin/%s", c.Branch)
	commits, err := repo.CommitsBetween(c.Since, remoteRef)
	if err != nil {
		return err
	}

	if len(commits) == 0 {
		logger.Noticef("No commits to replay (--since %s is already at remote HEAD)", c.Since)
		return nil
	}

	logger.Printf("Found %d commit(s) to replay\n", len(commits))

	changes, err := repo.Changes(commits...)
	if err != nil {
		return fmt.Errorf("get changes: %w", err)
	}

	return replayChanges(ctx, client, c.Since, changes...)
}

// replayChanges pushes changes with force-update enabled.
// baseCommit is used as the parent for the first replayed commit.
func replayChanges(ctx context.Context, client *Client, baseCommit string, changes ...Change) error {
	hashes := []string{}
	for i := 0; i < len(changes) && i < 10; i++ {
		hashes = append(hashes, changes[i].hash)
	}

	if len(changes) >= 10 {
		hashes = append(hashes, fmt.Sprintf("...and %d more.", len(changes)-10))
	}

	endGroup := logger.Group(fmt.Sprintf("Replaying to %s/%s (branch: %s)", client.owner, client.repo, client.branch))
	defer endGroup()

	logger.Printf("Commits to replay: %s\n", strings.Join(hashes, ", "))
	logger.Printf("Base commit: %s\n", baseCommit)

	pushed, newHead, err := client.PushChanges(ctx, baseCommit, changes...)
	if err != nil {
		return err
	} else if pushed != len(changes) {
		return fmt.Errorf("replayed %d of %d changes", pushed, len(changes))
	}

	logger.Noticef("Replayed %d commit(s) as signed: %s", len(changes), client.compareURL(baseCommit, newHead))

	// Output the new head reference for capture by callers or GitHub Actions
	if err := logger.Output("pushed_ref", newHead); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}
