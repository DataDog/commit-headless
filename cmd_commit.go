package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

type CommitCmd struct {
	remoteFlags

	Author  string   `help:"Specify an author using the standard 'A U Thor <author@example.com>' format."`
	Message []string `short:"m" help:"Specify a commit message. If used multiple times, values are concatenated as separate paragraphs."`
	Force   bool     `help:"Force commiting empty files. Only useful if you know you're deleting a file."`
	Files   []string `arg:"" help:"Files to commit."`
}

func (c *CommitCmd) Help() string {
	return `
This command can be used to create a single commit on the remote by passing in the names of files.

It is expected that the paths on disk match to paths on the remote. That is, if you supply
"path/to/file.txt" then the contents of that file on disk will be applied to that same file on the
remote when the commit is created.

You can also use this to delete files by passing a path to a file that does not exist on disk. Note
that for safety reasons, commit-headless will require an extra flag --force before accepting
deletions. It is an error to attempt to delete a file that does not exist.

If you pass a path to a file that does not exist on disk without the --force flag, commit-headless
will print an error and exit.

You can supply a commit message via --message/-m and an author via --author/-a. If unspecified,
default values will be used.

Examples:
	# Commit changes to these two files
	commit-headless commit [flags...] -- README.md .gitlab-ci.yml

	# Remove a file, add another one, and commit
	rm file/i/do/not/want
	echo "hello" > hi-there.txt
	commit-headless commit [flags...] --force -- hi-there.txt file/i/do/not/want

	# Commit a change with a custom message
	commit-headless commit [flags...] -m"ran a pipeline" -- output.txt
	`
}

func (c *CommitCmd) Run() error {
	change := Change{
		hash:    strings.Repeat("0", 40),
		author:  c.Author,
		message: strings.Join(c.Message, "\n\n"),
		entries: map[string][]byte{},
	}

	rootfs := os.DirFS(".")

	for _, path := range c.Files {
		path = strings.TrimPrefix(path, "./")

		fp, err := rootfs.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			if !c.Force {
				return fmt.Errorf("file %q does not exist, but --force was not set", path)
			}

			change.entries[path] = []byte{}
			continue
		} else if err != nil {
			return fmt.Errorf("could not open file %q: %w", path, err)
		}

		contents, err := io.ReadAll(fp)
		if err != nil {
			return fmt.Errorf("read %q: %w", path, err)
		}

		change.entries[path] = contents
	}

	owner, repository := c.Target.Owner(), c.Target.Repository()

	return pushChanges(context.Background(), owner, repository, c.Branch, c.BranchFrom, c.DryRun, change)
}
