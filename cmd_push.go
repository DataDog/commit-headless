package main

import (
	"context"
	"fmt"
	"os"
)

type PushCmd struct {
	remoteFlags
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

	# Force-push rebased commits onto an updated base
	commit-headless push -T owner/repo --branch feature --head-sha $(git rev-parse main) --force

When --head-sha is provided without --create-branch or --force, it acts as a safety check: the push
will fail if the remote branch HEAD doesn't match the expected value. This prevents accidentally
overwriting commits that were pushed after your workflow started.

When --force is used with --head-sha, the branch ref is updated even if the push is not a
fast-forward. The --head-sha value is used as the parent of the first pushed commit, bypassing the
remote HEAD check. This is useful for re-signing commits after a rebase.

The remote HEAD (or --head-sha when creating a branch or using --force) must be an ancestor of local
HEAD. If the histories have diverged (and --force is not set), the push will fail. This prevents
creating broken history when the local checkout is out of sync with the remote.

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

	token := getToken(os.Getenv)
	if token == "" {
		return fmt.Errorf("no GitHub token supplied")
	}

	client := NewClient(ctx, token, c.Target.Owner(), c.Target.Repository(), c.Branch)
	client.dryrun = c.DryRun
	client.force = c.Force

	baseCommit, err := c.ResolveBaseCommit(ctx, client)
	if err != nil {
		return err
	}

	// Find local commits that aren't on the remote (uses the logical base, before branch creation)
	commits, err := repo.CommitsSince(baseCommit)
	if err != nil {
		return err
	}

	if len(commits) == 0 {
		logger.Noticef("No local commits to push (local HEAD matches remote HEAD %s)", baseCommit)
		return nil
	}

	changes, err := repo.Changes(commits...)
	if err != nil {
		return fmt.Errorf("get changes: %w", err)
	}

	if c.CreateBranch {
		remoteSha, err := client.CreateBranch(ctx, baseCommit)
		if err != nil {
			return err
		}
		baseCommit = remoteSha
	}

	return pushChanges(ctx, client, baseCommit, changes...)
}
