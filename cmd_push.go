package main

import (
	"context"
	"fmt"
	"os"
)

type PushCmd struct {
	remoteFlags
	RepoPath string   `name:"repo-path" default:"." help:"Path to the repository that contains the commits. Defaults to the current directory."`
	Commits  []string `arg:"" optional:"" help:"Commit hashes to be applied to the target. Defaults to reading a list of commit hashes from standard input."`
}

func (c *PushCmd) Help() string {
	return `
This command should be run when you have commits created locally that you'd like to push to the
remote. You can pass the commit hashes either as space-separated arguments or over standard input
with one commit hash per line.

You must provide a GitHub token via the environment in one of the following variables, in preference
order:

	- HEADLESS_TOKEN
	- GITHUB_TOKEN
	- GH_TOKEN

On a successful push, the hash of the last commit pushed will be printed to standard output,
allowing you to capture it in a script. All other output is printed to standard error.

For example, to push the most recent three commits:

	commit-headless push -T owner/repo --branch branch HEAD HEAD^ HEAD^^

Or, to push all commits on the current branch that aren't on the main branch:

	git log --oneline main.. | commit-headless push -T owner/repo --branch branch

When reading commit hashes from standard input, the only requirement is that the commit hash is at
the start of the line, and any other content is separated by at least one whitespace character.

Note that the pushed commits will not share the same commit sha, and you should avoid operating on
the local checkout after running this command.

If, for some reason, you do need to craft new commits afterwards, or you need to interrogate the
pushed commits, you should hard reset the local checkout to the remote version after fetching:

	git fetch origin <branch>
	git reset --hard origin/<branch>
`
}

func (c *PushCmd) Run() error {
	if len(c.Commits) == 0 {
		var err error
		c.Commits, err = commitsFromStdin(os.Stdin)
		if err != nil {
			return err
		}
	}

	// Convert c.Commits into []Change which we can feed to the remote
	repo := &Repository{path: c.RepoPath}

	changes, err := repo.Changes(c.Commits...)
	if err != nil {
		return fmt.Errorf("get changes: %w", err)
	}

	owner, repository := c.Target.Owner(), c.Target.Repository()

	return pushChanges(context.Background(), owner, repository, c.Branch, c.DryRun, changes...)
}
