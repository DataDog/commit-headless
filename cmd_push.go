package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/kong"
)

type targetFlag string

func (f *targetFlag) Decode(ctx *kong.DecodeContext) error {
	if err := ctx.Scan.PopValueInto("string", &f); err != nil {
		return err
	}

	slashes := strings.Count(string(*f), "/")
	if slashes == 1 {
		return nil
	}

	return fmt.Errorf("must be of the form owner/repo with exactly one slash")
}

type PushCmd struct {
	Target  targetFlag `name:"target" short:"T" required:"" help:"Target repository in owner/repo format."`
	Branch  string     `required:"" help:"Name of the target branch on the remote."`
	RepoDir string     `name:"repo" default:"." help:"Path to the local repository that contains commits you want to push. Must not be a worktree."`
	DryRun  bool       `name:"dry-run" help:"Perform everything except the final remote writes to GitHub."`

	Commits []string `arg:"" optional:"" help:"Commit hashes to be applied to the target. Defaults to reading a list of commit hashes from standard input."`
}

func (c *PushCmd) Run() error {
	if len(c.Commits) == 0 {
		var err error
		c.Commits, err = commitsFromStdin(os.Stdin)
		if err != nil {
			return err
		}
	}

	owner, repository, _ := strings.Cut(string(c.Target), "/")

	commits := c.Commits[:]
	if len(c.Commits) >= 10 {
		commits = commits[:10]
		commits = append(commits, fmt.Sprintf("...and %d more.", len(c.Commits)-10))
	}

	commitsout := strings.Join(commits, ", ")

	log("Owner: %s\n", owner)
	log("Repository: %s\n", repository)
	log("Branch: %s\n", c.Branch)
	log("Commits: %s\n", commitsout)

	return push(c.RepoDir, owner, repository, c.Branch, c.Commits, c.DryRun)
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

// push actually performs the push
func push(gitdir, owner, repository, branch string, commits []string, dryrun bool) error {
	token := getToken(os.Getenv)
	if token == "" {
		return errors.New("no GitHub token supplied")
	}

	client := NewClient(context.Background(), token, owner, repository, branch)
	client.dryrun = dryrun

	headRef, err := client.GetHeadCommitHash(context.Background())
	if err != nil {
		return err
	}

	log("Current head commit: %s\n", headRef)

	repo := &Repository{path: gitdir}

	changes, err := repo.Changes(commits...)
	if err != nil {
		return fmt.Errorf("get changes: %w", err)
	}

	for _, c := range changes {
		log("Commit %s\n", c.hash)
		log("  Headline: %s\n", c.Headline)
		log("  Body: %s\n", c.Body)
		log("  Changed files: %d\n", len(c.Changes))
		for p, content := range c.Changes {
			action := "MODIFY"
			if len(content) == 0 {
				action = "DELETE"
			}
			log("    - %s: %s\n", action, p)
		}
	}

	pushed, newHead, err := client.PushChanges(context.Background(), headRef, changes...)
	if err != nil {
		return err
	} else if pushed != len(changes) {
		return fmt.Errorf("pushed %d of %d changes", pushed, len(changes))
	}

	log("Pushed %d commits.\n", len(changes))
	log("Branch URL: %s\n", client.browseCommitsURL())

	// The only thing that goes to standard output is the new head reference, allowing callers to
	// capture stdout if they need the reference.
	fmt.Println(newHead)

	return nil
}
