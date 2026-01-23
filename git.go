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

// CommitsSince returns the commits between base and HEAD, oldest first.
// This is equivalent to `git rev-list --reverse base..HEAD`.
// Returns an error if base is not an ancestor of HEAD.
func (r *Repository) CommitsSince(base string) ([]string, error) {
	// First verify that base is an ancestor of HEAD
	cmd := exec.Command("git", "merge-base", "--is-ancestor", base, "HEAD")
	cmd.Dir = r.path
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("remote HEAD %s is not an ancestor of local HEAD (histories have diverged)", base)
		}
		return nil, fmt.Errorf("check ancestry: %w", err)
	}

	cmd = exec.Command("git", "rev-list", "--reverse", base+"..HEAD")
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("list commits: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("list commits: %w", err)
	}

	var commits []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		commits = append(commits, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return commits, nil
}

// Returns a Change for each supplied commit hash
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

// Returns a Change for the specific commit hash
func (r *Repository) changed(commit string) (Change, error) {
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
		entries: map[string]FileEntry{},
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
				logger.Warningf("Author is malformed (%s), using placeholder", value)
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

// Returns the files changed in the given commit, along with their contents and modes.
// Deleted files will have nil content.
func (r *Repository) changedFiles(commit string) (map[string]FileEntry, error) {
	cmd := exec.Command("git", "diff-tree", "--no-commit-id", "--name-status", "-r", commit)
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	changes := map[string]FileEntry{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		ln := scanner.Text()

		status, value, _ := strings.Cut(ln, "\t")
		switch {
		case status == "A" || status == "M":
			content, mode, err := r.fileContentAndMode(commit, value)
			if err != nil {
				return nil, fmt.Errorf("get content %s:%s: %w", commit, value, err)
			}
			changes[value] = FileEntry{Content: content, Mode: mode}
		case strings.HasPrefix(status, "R"): // Renames may have a similarity score after the R
			from, to, _ := strings.Cut(value, "\t")
			changes[from] = FileEntry{Content: nil, Mode: ""}
			content, mode, err := r.fileContentAndMode(commit, to)
			if err != nil {
				return nil, fmt.Errorf("get content %s:%s: %w", commit, to, err)
			}
			changes[to] = FileEntry{Content: content, Mode: mode}
		case status == "D":
			changes[value] = FileEntry{Content: nil, Mode: ""}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return changes, nil
}

func (r *Repository) fileContentAndMode(commit, path string) ([]byte, string, error) {
	// Get the file mode from ls-tree
	cmd := exec.Command("git", "ls-tree", commit, "--", path)
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("ls-tree: %w", err)
	}

	// Output format: mode SP type SP hash TAB path
	mode := strings.SplitN(string(out), " ", 2)[0]

	// Get the file content
	cmd = exec.Command("git", "cat-file", "blob", fmt.Sprintf("%s:%s", commit, path))
	cmd.Dir = r.path
	content, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("cat-file: %w", err)
	}

	return content, mode, nil
}

// StagedChanges returns the files staged for commit along with their contents and modes.
// Deleted files have nil content. Returns an empty map if there are no staged changes.
func (r *Repository) StagedChanges() (map[string]FileEntry, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-status")
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("get staged changes: %w", err)
	}

	changes := map[string]FileEntry{}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		ln := scanner.Text()
		if ln == "" {
			continue
		}

		status, path, _ := strings.Cut(ln, "\t")
		switch {
		case status == "A" || status == "M":
			content, mode, err := r.stagedContentAndMode(path)
			if err != nil {
				return nil, fmt.Errorf("get staged content %s: %w", path, err)
			}
			changes[path] = FileEntry{Content: content, Mode: mode}
		case strings.HasPrefix(status, "R"): // Renames have the form "Rxxx\told\tnew"
			from, to, _ := strings.Cut(path, "\t")
			changes[from] = FileEntry{Content: nil, Mode: ""}
			content, mode, err := r.stagedContentAndMode(to)
			if err != nil {
				return nil, fmt.Errorf("get staged content %s: %w", to, err)
			}
			changes[to] = FileEntry{Content: content, Mode: mode}
		case status == "D":
			changes[path] = FileEntry{Content: nil, Mode: ""}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return changes, nil
}

func (r *Repository) stagedContentAndMode(path string) ([]byte, string, error) {
	// Get mode from ls-files -s (format: mode SP hash SP stage TAB path)
	cmd := exec.Command("git", "ls-files", "-s", "--", path)
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("ls-files: %w", err)
	}

	mode := strings.SplitN(string(out), " ", 2)[0]

	// Get content from the index
	cmd = exec.Command("git", "cat-file", "blob", ":"+path)
	cmd.Dir = r.path
	content, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("cat-file: %w", err)
	}

	return content, mode, nil
}
