package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type Repository struct {
	path string
}

// Returns a Change for each supplied commit
func (r *Repository) Changes(commits ...string) ([]Change, error) {
	changes := make([]Change, len(commits))
	for i, h := range commits {
		change, err := r.changed(h)
		if err != nil {
			return nil, fmt.Errorf("get change %s: %w", h, err)
		}
		changes[i] = change
	}
	return changes, nil
}

// Returns a Change for the specific commit
func (r *Repository) changed(commit string) (Change, error) {
	// First, make sure the commit looks like a commit hash
	// While technically all of our calls would work with references such as HEAD,
	// refs/heads/branch, refs/tags/etc we're going to require callers provide things that look like
	// commits.
	if !hashRegex.MatchString(commit) {
		return Change{}, fmt.Errorf("commit %q does not look like a commit, should be at least 4 hexadecimal digits.", commit)
	}

	parents, author, message, err := r.catfile(commit)
	if err != nil {
		return Change{}, err
	}

	if len(parents) > 1 {
		return Change{}, fmt.Errorf("range includes a merge commit (%s), not continuing", commit)
	}

	change := Change{
		hash:    commit,
		message: message,
		author:  author,
		entries: map[string][]byte{},
	}

	change.entries, err = r.changedFiles(commit)
	if err != nil {
		return Change{}, err
	}

	return change, nil
}

func (r *Repository) catfile(commit string) ([]string, string, string, error) {
	cmd := exec.Command("git", "cat-file", "commit", commit)
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		return nil, "", "", err
	}

	parents := []string{}
	author, message := "", ""

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		ln := scanner.Text()

		// End of headers, start of message
		if ln == "" {
			break
		}

		key, value, _ := strings.Cut(ln, " ")

		switch key {
		case "parent":
			parents = append(parents, value)
		case "author":
			// author line is First Last <email@domain.com> timestamp timezone
			// so we can just grab up to the last >
			marker := strings.LastIndex(value, ">")
			if marker == -1 {
				// no author, or malformed, so make one up
				log("Author is malformed, using a placeholder.\n")
				log("  Malformed: %s\n", value)
				author = "Commit Headless <commit-headless-bot@datadoghq.com>"
			} else {
				author = value[:marker+1]
			}
		}
	}

	mb := &strings.Builder{}
	for scanner.Scan() {
		mb.WriteString(scanner.Text())
		mb.WriteString("\n")
	}

	message = strings.TrimSpace(mb.String())

	if err := scanner.Err(); err != nil {
		return nil, "", "", err
	}

	return parents, author, message, nil
}

// Returns the files changed in the given commit, along with their contents
// Deleted files will have an empty value
func (r *Repository) changedFiles(commit string) (map[string][]byte, error) {
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-status", "-r", commit)
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	changes := map[string][]byte{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		ln := scanner.Text()

		status, value, _ := strings.Cut(ln, "\t")
		switch {
		case status == "A" || status == "M":
			contents, err := r.fileContent(commit, value)
			if err != nil {
				return nil, fmt.Errorf("get content %s:%s: %w", commit, value, err)
			}
			changes[value] = contents
		case strings.HasPrefix(status, "R"): // Renames may have a similarity score after the R
			from, to, _ := strings.Cut(value, "\t")
			changes[from] = nil
			contents, err := r.fileContent(commit, to)
			if err != nil {
				return nil, fmt.Errorf("get content %s:%s: %w", commit, to, err)
			}
			changes[to] = contents
		case status == "D":
			changes[value] = nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return changes, nil
}

func (r *Repository) fileContent(commit, path string) ([]byte, error) {
	cmd := exec.Command("git", "cat-file", "blob", fmt.Sprintf("%s:%s", commit, path))
	cmd.Dir = r.path
	return cmd.Output()
}
