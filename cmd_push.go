package main

import (
	"context"
	"fmt"
	"os"
)

type PushCmd struct {
	remoteFlags
	RepoPath string `name:"repo-path" default:"." help:"Path to the repository that contains the commits. Defaults to the current directory."`
}

func (c *PushCmd) Help() string {
	return `
This command pushes local commits that don't exist on the remote branch. It automatically determines
which commits need to be pushed by comparing local HEAD with the remote branch HEAD.

You must provide a GitHub token via the environment in one of the following variables, in preference
order:

	- HEADLESS_TOKEN
	- GITHUB_TOKEN
	- GH_TOKEN

On a successful push, the hash of the last commit pushed will be printed to standard output,
allowing you to capture it in a script. All other output is printed to standard error.

Example usage:

	# Push local commits to an existing remote branch
	commit-headless push -T owner/repo --branch feature

	# Create a new branch from a specific commit and push local commits
	commit-headless push -T owner/repo --branch new-feature --head-sha abc123 --create-branch

	# Push with a safety check that remote HEAD matches expected value
	commit-headless push -T owner/repo --branch feature --head-sha abc123

When --head-sha is provided without --create-branch, it acts as a safety check: the push will fail
if the remote branch HEAD doesn't match the expected value. This prevents accidentally overwriting
commits that were pushed after your workflow started.

The remote HEAD (or --head-sha when creating a branch) must be an ancestor of local HEAD. If the
histories have diverged, the push will fail. This prevents creating broken history when the local
checkout is out of sync with the remote.

Note that the pushed commits will not share the same commit sha, and you should avoid operating on
the local checkout after running this command.

If, for some reason, you do need to craft new commits afterwards, or you need to interrogate the
pushed commits, you should hard reset the local checkout to the remote version after fetching:

	git fetch origin <branch>
	git reset --hard origin/<branch>
`
}

func (c *PushCmd) Run() error {
	ctx := context.Background()
	repo := &Repository{path: c.RepoPath}
	owner, repository := c.Target.Owner(), c.Target.Repository()

	// Determine the base commit (remote HEAD or --head-sha for new branches)
	baseCommit, err := c.getBaseCommit(ctx, owner, repository)
	if err != nil {
		return err
	}

	// Find local commits that aren't on the remote
	commits, err := repo.CommitsSince(baseCommit)
	if err != nil {
		return err
	}

	if len(commits) == 0 {
		log("No local commits to push (local HEAD matches remote HEAD %s)\n", baseCommit)
		return nil
	}

	changes, err := repo.Changes(commits...)
	if err != nil {
		return fmt.Errorf("get changes: %w", err)
	}

	return pushChanges(ctx, owner, repository, c.Branch, c.HeadSha, c.CreateBranch, c.DryRun, changes...)
}

// getBaseCommit returns the commit to use as the base for determining what to push.
// For new branches (--create-branch), this is --head-sha.
// For existing branches, this is the remote HEAD (validated against --head-sha if provided).
func (c *PushCmd) getBaseCommit(ctx context.Context, owner, repository string) (string, error) {
	if c.CreateBranch {
		if c.HeadSha == "" {
			return "", fmt.Errorf("--create-branch requires --head-sha to specify the branch point")
		}
		return c.HeadSha, nil
	}

	// Get the remote branch HEAD
	token := getToken(os.Getenv)
	if token == "" {
		return "", fmt.Errorf("no GitHub token supplied")
	}

	client := NewClient(ctx, token, owner, repository, c.Branch)
	remoteHead, err := client.GetHeadCommitHash(ctx)
	if err != nil {
		return "", fmt.Errorf("get remote HEAD: %w", err)
	}

	// If --head-sha was provided, validate it matches the remote
	if c.HeadSha != "" && c.HeadSha != remoteHead {
		return "", fmt.Errorf("remote HEAD %s doesn't match expected --head-sha %s (the branch may have been updated)", remoteHead, c.HeadSha)
	}

	return remoteHead, nil
}
