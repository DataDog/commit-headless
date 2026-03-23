package main

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/alecthomas/kong"
)

var logger *Logger
var hashRegex = regexp.MustCompile(`^[a-f0-9]{4,40}$`)

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

func (f targetFlag) Owner() string {
	owner, _, _ := strings.Cut(string(f), "/")
	return owner
}

func (f targetFlag) Repository() string {
	_, repo, _ := strings.Cut(string(f), "/")
	return repo
}

// baseFlags are shared among all commands that interact with the remote.
type baseFlags struct {
	Target   targetFlag `name:"target" short:"T" required:"" help:"Target repository in owner/repo format."`
	Branch   string     `required:"" help:"Name of the target branch on the remote."`
	HeadSha  string     `name:"head-sha" help:"Expected commit sha of the remote branch, or the commit sha to branch from."`
	DryRun   bool       `name:"dry-run" help:"Perform everything except the final remote writes to GitHub."`
	RepoPath string     `name:"repo-path" default:"." help:"Path to the local repository. Defaults to the current directory."`
}

// ValidateHeadSha checks that --head-sha is a valid full-length hex commit hash, if provided.
func (f *baseFlags) ValidateHeadSha() error {
	if f.HeadSha != "" && (!hashRegex.MatchString(f.HeadSha) || len(f.HeadSha) != 40) {
		return fmt.Errorf("invalid head-sha %q, must be a full 40 hex digit commit hash", f.HeadSha)
	}
	return nil
}

// ValidateRemoteHead fetches the remote branch HEAD and checks it matches --head-sha if provided.
// Returns the remote HEAD hash.
func (f *baseFlags) ValidateRemoteHead(ctx context.Context, client *Client) (string, error) {
	remoteHead, err := client.GetHeadCommitHash(ctx)
	if err != nil {
		return "", fmt.Errorf("get remote HEAD: %w", err)
	}
	if f.HeadSha != "" && f.HeadSha != remoteHead {
		return "", fmt.Errorf("remote HEAD %s doesn't match expected --head-sha %s (the branch may have been updated)", remoteHead, f.HeadSha)
	}
	return remoteHead, nil
}

// remoteFlags extends baseFlags with options for push-style commands that create commits.
type remoteFlags struct {
	baseFlags
	CreateBranch bool `name:"create-branch" help:"Create the remote branch, requires --head-sha to be set."`
	Force        bool `help:"Force-update the branch ref, allowing non-fast-forward updates. Requires --head-sha to be set."`
}

// ResolveBaseCommit determines the commit to use as the parent of the first pushed commit.
// For --create-branch: returns --head-sha as the branch point.
// For --force: returns --head-sha directly, skipping remote HEAD validation.
// Otherwise: fetches the remote HEAD, validating against --head-sha if provided.
func (f *remoteFlags) ResolveBaseCommit(ctx context.Context, client *Client) (string, error) {
	if err := f.ValidateHeadSha(); err != nil {
		return "", err
	}

	if f.CreateBranch {
		if f.HeadSha == "" {
			return "", fmt.Errorf("--create-branch requires --head-sha to specify the branch point")
		}
		return f.HeadSha, nil
	}

	if f.Force {
		if f.HeadSha == "" {
			return "", fmt.Errorf("--force requires --head-sha to specify the new base commit")
		}
		return f.HeadSha, nil
	}

	return f.ValidateRemoteHead(ctx, client)
}

type CLI struct {
	Push    PushCmd    `cmd:"" help:"Push local commits to the remote."`
	Commit  CommitCmd  `cmd:"" help:"Create a commit directly on the remote."`
	Replay  ReplayCmd  `cmd:"" help:"Replay remote commits as signed commits."`
	Version VersionCmd `cmd:"" help:"Print version information and exit."`
}

func main() {
	logger = NewLogger(os.Stderr)

	cli := CLI{}

	ctx := kong.Parse(&cli,
		kong.Name("commit-headless"),
		kong.Description("A tool to create signed commits on GitHub."),
		kong.UsageOnError(),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
