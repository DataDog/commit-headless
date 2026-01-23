package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

type CommitCmd struct {
	remoteFlags

	RepoPath string   `name:"repo-path" default:"." help:"Path to the repository. Defaults to the current directory."`
	Author   string   `help:"Specify an author using the standard 'A U Thor <author@example.com>' format."`
	Message  []string `short:"m" help:"Specify a commit message. If used multiple times, values are concatenated as separate paragraphs."`
}

func (c *CommitCmd) Help() string {
	return `
This command creates a single commit on the remote from the currently staged changes (git add).

It works like 'git commit' - stage your changes first, then run this command to push them as a
signed commit on the remote.

The staged file paths must match the paths on the remote. That is, if you stage "path/to/file.txt"
then the contents of that file will be applied to that same path on the remote.

Staged deletions (git rm) are also supported.

You can supply a commit message via --message/-m and an author via --author. If unspecified,
default values will be used.

Unlike 'push', this command does not require any relationship between local and remote history.
This makes it useful for broadcasting the same file changes to multiple repositories:

	git add config.yml security-policy.md
	commit-headless commit -T org/repo1 --branch main -m "Update security policy"
	commit-headless commit -T org/repo2 --branch main -m "Update security policy"
	commit-headless commit -T org/repo3 --branch main -m "Update security policy"

Each target repository can have completely unrelated history - you're applying file contents,
not replaying commits.

Examples:
	# Stage changes and commit to remote
	git add README.md .gitlab-ci.yml
	commit-headless commit -T owner/repo --branch feature -m "Update docs"

	# Stage a deletion and a new file
	git rm old-file.txt
	git add new-file.txt
	commit-headless commit -T owner/repo --branch feature -m "Replace old with new"

	# Stage all changes and commit
	git add -A
	commit-headless commit -T owner/repo --branch feature -m "Update everything"
`
}

func (c *CommitCmd) Run() error {
	repo := &Repository{path: c.RepoPath}

	entries, err := repo.StagedChanges()
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		logger.Notice("No staged changes to commit")
		return nil
	}

	change := Change{
		hash:    strings.Repeat("0", 40),
		author:  c.Author,
		message: strings.Join(c.Message, "\n\n"),
		entries: entries,
	}

	ctx := context.Background()
	owner, repository := c.Target.Owner(), c.Target.Repository()

	// Validate --head-sha against remote HEAD (same safety check as push command)
	if c.HeadSha != "" && !c.CreateBranch {
		token := getToken(os.Getenv)
		if token == "" {
			return fmt.Errorf("no GitHub token supplied")
		}
		client := NewClient(ctx, token, owner, repository, c.Branch)
		remoteHead, err := client.GetHeadCommitHash(ctx)
		if err != nil {
			return fmt.Errorf("get remote HEAD: %w", err)
		}
		if c.HeadSha != remoteHead {
			return fmt.Errorf("remote HEAD %s doesn't match expected --head-sha %s (the branch may have been updated)", remoteHead, c.HeadSha)
		}
	}

	return pushChanges(ctx, owner, repository, c.Branch, c.HeadSha, c.CreateBranch, c.DryRun, change)
}
