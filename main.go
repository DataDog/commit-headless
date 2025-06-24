package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/alecthomas/kong"
)

var hexdigits = regexp.MustCompile(`[a-f0-9]{4,}`)

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

var CLI struct {
	Push PushCmd `cmd:"" help:"Push local commits to the remote."`
}

type PushCmd struct {
	Target  targetFlag `name:"repo" short:"R" required:"" help:"Target repository in owner/repo format."`
	Branch  string     `required:"" help:"Name of the target branch on the remote."`
	Commits []string   `arg:"" optional:"" help:"Commit hashes to be applied to the target. Defaults to reading a list of commit hashes from standard input."`
}

func (c *PushCmd) Run() error {
	if len(c.Commits) == 0 {
		if !stdinIsPiped() {
			return errors.New("you must pipe a list of commits to standard input or supply commits as arguments.")
		}

		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			ln := scanner.Text()
			fs := strings.Fields(ln)
			if len(fs) == 0 {
				return fmt.Errorf("could not parse commit hash from standard input: %q", ln)
			}

			if hexdigits.MatchString(fs[0]) {
				c.Commits = append(c.Commits, fs[0])
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}

		// reverse the commits since log output is newest first
		// TODO: Should this be detected by commit time instead?
		slices.Reverse(c.Commits)
	}

	fmt.Printf("repository: %s\n", c.Target)
	fmt.Printf("branch: %s\n", c.Branch)
	fmt.Printf("commits: %q\n", c.Commits)
	return push(string(c.Target), c.Branch, c.Commits)
}

func stdinIsPiped() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return fi.Mode()&os.ModeNamedPipe != 0
}

func (c *PushCmd) Help() string {
	return `
This command should be run when you have commits created locally that you'd like to push to the
remote. You can pass the commit hashes either as space-separated arguments or over standard input
with one commit hash per line.

For example, to push the most recent three commits:

	commit-headless push HEAD HEAD^ HEAD^^

Or, to push all commits on the current branch that aren't on the main branch:

	git log --oneline main.. | commit-headless push

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

func main() {
	ctx := kong.Parse(&CLI)
	ctx.FatalIfErrorf(ctx.Run())
}
