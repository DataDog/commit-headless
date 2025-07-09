package main

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
)

type stattable interface {
	Stat() (fs.FileInfo, error)
}

var (
	errUnpipedStdin   = errors.New("could not read from non-piped standard input")
	errStatStdin      = errors.New("could not read commits from standard input")
	errNoCommitsStdin = errors.New("no commits present on standard input")
)

var hashRegex = regexp.MustCompile(`^[a-f0-9]{4,40}$`)

// reads a list of commit hashes from r, which is typically stdin, returning the commit hashes in
// reverse order
func commitsFromStdin(r io.Reader) ([]string, error) {
	// if r is stattable (like os.Stdin), make sure it's a pipe
	if stdin, ok := r.(stattable); ok {
		fi, err := stdin.Stat()
		if err != nil {
			return nil, errStatStdin
		}

		if fi.Mode()&os.ModeNamedPipe == 0 {
			return nil, errUnpipedStdin
		}
	}

	commits := []string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		ln := scanner.Text()
		fs := strings.Fields(ln)

		// the only time strings.Fields returns an empty slice is when the input is only spaces
		// so we'll just continue
		if len(fs) == 0 {
			continue
		}

		if hashRegex.MatchString(fs[0]) {
			commits = append(commits, fs[0])
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(commits) == 0 {
		return nil, errNoCommitsStdin
	}

	// reverse the commits since log output is newest first
	// TODO: Should this be detected by commit time instead?
	slices.Reverse(commits)
	return commits, nil
}
