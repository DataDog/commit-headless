package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/kong"
)

var logwriter io.Writer

func log(f string, args ...any) {
	fmt.Fprintf(logwriter, f, args...)
}

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

type remoteFlags struct {
	Target targetFlag `name:"target" short:"T" required:"" help:"Target repository in owner/repo format."`
	Branch string     `required:"" help:"Name of the target branch on the remote."`
	DryRun bool       `name:"dry-run" help:"Perform everything except the final remote writes to GitHub."`
}

type CLI struct {
	Push    PushCmd    `cmd:"" help:"Push local commits to the remote."`
	Version VersionCmd `cmd:"" help:"Print version information and exit."`
}

func main() {
	logwriter = os.Stderr

	cli := CLI{}

	ctx := kong.Parse(&cli,
		kong.Name("commit-headless"),
		kong.Description("A tool to create signed commits on GitHub."),
		kong.UsageOnError(),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
