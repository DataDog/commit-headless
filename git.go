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

// Fetch fetches the specified branch from origin.
func (r *Repository) Fetch(branch string) error {
	return r.FetchFrom("origin", branch)
}

// FetchFrom fetches the given refspecs from the named remote.
func (r *Repository) FetchFrom(remote string, refspecs ...string) error {
	args := append([]string{"fetch", remote}, refspecs...)
	cmd := exec.Command("git", args...)
	cmd.Dir = r.path
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("fetch: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("fetch: %w", err)
	}
	return nil
}

// IsClean reports whether the working tree has any tracked-file changes (staged
// or unstaged). Untracked files do not count as dirty, since `git reset --hard`
// leaves them in place. The returned summary lists the dirty paths for logging.
func (r *Repository) IsClean() (bool, string, error) {
	cmd := exec.Command("git", "status", "--porcelain=v1")
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return false, "", fmt.Errorf("status: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return false, "", fmt.Errorf("status: %w", err)
	}

	var dirty []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		ln := scanner.Text()
		if len(ln) < 3 || strings.HasPrefix(ln, "??") {
			// Skip blank lines and untracked entries.
			continue
		}
		dirty = append(dirty, ln)
	}
	if err := scanner.Err(); err != nil {
		return false, "", err
	}

	if len(dirty) == 0 {
		return true, "", nil
	}
	return false, strings.Join(dirty, "\n"), nil
}

// CurrentBranch returns the name of the local branch HEAD is on, or an empty
// string if HEAD is detached.
func (r *Repository) CurrentBranch() (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "--quiet", "--short", "HEAD")
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		// `symbolic-ref --quiet` exits 1 silently when HEAD is detached.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("symbolic-ref: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ResetHard runs `git reset --hard <ref>`.
func (r *Repository) ResetHard(ref string) error {
	cmd := exec.Command("git", "reset", "--hard", ref)
	cmd.Dir = r.path
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("reset: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return fmt.Errorf("reset: %w", err)
	}
	return nil
}

// RemoteForTarget returns the name of the unique git remote whose URL points
// at github.com/<owner>/<repo>. Match is case-insensitive and accepts SSH,
// HTTPS, and git:// URL forms with or without a trailing .git suffix.
//
// Returns an error if zero or multiple remotes match. On the no-match case the
// error includes the remotes it actually parsed from `git remote -v`, so that
// environments that rewrite display URLs (e.g., url.<base>.insteadOf) make
// their effect visible.
func (r *Repository) RemoteForTarget(owner, repo string) (string, error) {
	cmd := exec.Command("git", "remote", "-v")
	cmd.Dir = r.path
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("list remotes: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("list remotes: %w", err)
	}

	target := strings.ToLower(fmt.Sprintf("github.com/%s/%s", owner, repo))
	seen := map[string]string{}
	var seenOrder []string
	var matches []string

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		name, url := fields[0], fields[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = url
		seenOrder = append(seenOrder, name)
		if remoteURLMatchesTarget(url, target) {
			matches = append(matches, name)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no git remote points at github.com/%s/%s (saw: %s)", owner, repo, formatRemotes(seen, seenOrder))
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple git remotes point at github.com/%s/%s: %s", owner, repo, strings.Join(matches, ", "))
	}
}

func formatRemotes(remotes map[string]string, order []string) string {
	if len(order) == 0 {
		return "no remotes configured"
	}
	pairs := make([]string, 0, len(order))
	for _, name := range order {
		pairs = append(pairs, fmt.Sprintf("%s=%s", name, remotes[name]))
	}
	return strings.Join(pairs, ", ")
}

// remoteURLMatchesTarget reports whether url refers to target, where target is
// the lower-cased "github.com/<owner>/<repo>" form. Handles trailing .git and
// the SSH "github.com:" separator.
func remoteURLMatchesTarget(url, target string) bool {
	url = strings.ToLower(strings.TrimSuffix(url, ".git"))
	// Normalize SSH-style "github.com:owner/repo" to "github.com/owner/repo".
	if i := strings.Index(url, "github.com:"); i >= 0 {
		url = url[:i] + "github.com/" + url[i+len("github.com:"):]
	}
	return strings.HasSuffix(url, target)
}

// CommitsBetween returns the commits between base and head, oldest first.
// This is equivalent to `git rev-list --reverse base..head`.
// Returns an error if base is not an ancestor of head.
func (r *Repository) CommitsBetween(base, head string) ([]string, error) {
	// First verify that base is an ancestor of head
	cmd := exec.Command("git", "merge-base", "--is-ancestor", base, head)
	cmd.Dir = r.path
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s is not an ancestor of %s (histories have diverged)", base, head)
		}
		return nil, fmt.Errorf("check ancestry: %w", err)
	}

	cmd = exec.Command("git", "rev-list", "--reverse", base+".."+head)
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
	fields := strings.SplitN(string(out), " ", 3)
	if len(fields) < 3 {
		return nil, "", fmt.Errorf("ls-tree: unexpected output %q", out)
	}
	mode := fields[0]
	hash := strings.SplitN(fields[2], "\t", 2)[0]

	if mode == modeSubmodule {
		// Submodules are gitlinks: the tree entry's hash is a commit in the
		// submodule's own history, not an object in this repository, so
		// there is no blob content to read via cat-file.
		return []byte(hash), mode, nil
	}

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

	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return nil, "", fmt.Errorf("ls-files: unexpected output %q", out)
	}
	mode, hash := fields[0], fields[1]

	if mode == modeSubmodule {
		// Submodules are gitlinks: the indexed hash is a commit in the
		// submodule's own history, not an object in this repository, so
		// there is no blob content to read via cat-file.
		return []byte(hash), mode, nil
	}

	// Get content from the index
	cmd = exec.Command("git", "cat-file", "blob", ":"+path)
	cmd.Dir = r.path
	content, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("cat-file: %w", err)
	}

	return content, mode, nil
}
