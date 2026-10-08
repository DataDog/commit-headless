package main

// git-remote-headless is a git remote helper (see gitremote-helpers(7)) that makes a plain
// `git push` land GitHub-signed commits, for machines without a signing key. See the README.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const remoteHelperDocs = "https://github.com/DataDog/commit-headless#git-remote-headless"

// headlessPush recreates changes on top of base through the API and points branch at them,
// returning the new head. It's a variable so tests can replace GitHub.
var headlessPush = func(ctx context.Context, token string, target targetFlag, branch, base string, create, force bool, tree string, changes []Change) (string, error) {
	client := NewClient(ctx, token, target.Owner(), target.Repository(), branch)
	client.signAttempts = 5
	client.force, client.expectedTree = force, tree
	if create {
		if _, err := client.CreateBranch(ctx, base); err != nil {
			return "", err
		}
	}
	_, head, err := client.PushChanges(ctx, base, changes...)
	return head, err
}

type remoteHelper struct {
	remote     string // remote name, or the URL itself for `git push <url>`
	url        string
	target     targetFlag
	out        io.Writer
	remoteRefs map[string]string
	leases     map[string]string // --force-with-lease: ref -> expected sha, "" for "must not exist"
}

// remoteHelperMain is the entry point when git runs git-remote-headless <remote> <url>.
func remoteHelperMain(args []string) int {
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "git-remote-headless is invoked by git, see %s\n", remoteHelperDocs)
		return 1
	}
	target, err := parseGitHubURL(args[1])
	if err == nil {
		logger = NewLogger(io.Discard)
		err = (&remoteHelper{remote: args[0], url: args[1], target: target, out: os.Stdout}).serve(os.Stdin)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "headless: %s (see %s)\n", err, remoteHelperDocs)
		return 1
	}
	return 0
}

// serve speaks the remote helper protocol until git closes stdin or sends a blank line.
func (h *remoteHelper) serve(in io.Reader) error {
	h.remoteRefs, h.leases = map[string]string{}, map[string]string{}
	var batch []string
	scanner := bufio.NewScanner(in)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "capabilities":
			fmt.Fprint(h.out, "push\noption\n\n")
		case strings.HasPrefix(line, "option cas "):
			ref, sha, _ := strings.Cut(strings.TrimPrefix(line, "option cas "), ":")
			if strings.Trim(sha, "0") == "" {
				sha = "" // the zero OID means "must not exist"
			}
			h.leases[ref] = sha
			fmt.Fprintln(h.out, "ok")
		case strings.HasPrefix(line, "option "):
			fmt.Fprintln(h.out, "unsupported") // git then refuses e.g. --dry-run itself
		case line == "list for-push":
			if err := h.list(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "push "):
			batch = append(batch, strings.TrimPrefix(line, "push "))
		case line == "" && batch != nil:
			for _, refspec := range batch {
				fmt.Fprint(h.out, h.push(refspec))
			}
			fmt.Fprintln(h.out)
			batch = nil
		case line == "":
			return nil
		default:
			return fmt.Errorf("unsupported command %q", line)
		}
	}
	return scanner.Err()
}

// list prints the remote branches and tags, which git uses for up-to-date and fast-forward checks.
func (h *remoteHelper) list() error {
	out, err := git("", "ls-remote", h.url, "refs/heads/*", "refs/tags/*")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		if sha, ref, ok := strings.Cut(line, "\t"); ok {
			h.remoteRefs[ref] = sha
			if !strings.HasSuffix(ref, "^{}") { // peeled tags only serve to exclude known commits
				fmt.Fprintf(h.out, "%s %s\n", sha, ref)
			}
		}
	}
	fmt.Fprintln(h.out)
	return nil
}

// push handles one refspec and returns its protocol status lines.
func (h *remoteHelper) push(refspec string) string {
	src, dst, _ := strings.Cut(refspec, ":")
	newHead, err := h.pushOne(strings.TrimPrefix(src, "+"), dst, strings.HasPrefix(src, "+"))
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "headless: %s: %s (see %s)\n", dst, err, remoteHelperDocs)
		return fmt.Sprintf("error %s %q\n", dst, err.Error())
	case newHead != "":
		// Have git record the signed commit, not the pushed one, in the remote-tracking ref.
		return fmt.Sprintf("ok %s\noption new-oid %s\n", dst, newHead)
	default:
		return fmt.Sprintf("ok %s\n", dst)
	}
}

// pushOne returns the new remote head when commits were recreated, or "" for a regular push.
func (h *remoteHelper) pushOne(src, dst string, force bool) (string, error) {
	old := h.remoteRefs[dst]
	if lease, ok := h.leases[dst]; ok {
		if lease != old {
			return "", errors.New("stale info")
		}
		force = true
	}
	if src == "" || !strings.HasPrefix(dst, "refs/heads/") { // deletions and tags
		return "", h.nativePush(src, dst, force)
	}
	local, err := git("", "rev-parse", "--verify", src+"^{commit}")
	if err != nil {
		return "", err
	}
	// Outgoing commits are reachable from local but from no remote tip. Remote-tracking refs also
	// count, in case the remote moved since the last fetch and its new tips are unknown here.
	exclude := ""
	for _, sha := range h.remoteRefs {
		exclude += "^" + sha + "\n"
	}
	out, err := git(exclude, "rev-list", "--reverse", "--ignore-missing", "--stdin", local, "--not", "--remotes="+h.remote)
	outgoing := strings.Fields(out)
	if err != nil {
		return "", err
	}
	// Signed commits (normally signed through the forwarded SSH agent) are pushed as they are,
	// and only the commits from the first unsigned one onward are recreated.
	signed := 0
	for signed < len(outgoing) && isSigned(outgoing[signed]) {
		signed++
	}
	if signed == len(outgoing) {
		return "", h.nativePush(src, dst, force)
	}
	if signed > 0 {
		if err := h.nativePush(outgoing[signed-1], dst, force); err != nil {
			return "", err
		}
		old, force, outgoing = outgoing[signed-1], false, outgoing[signed:]
	}

	base, err := git("", "rev-parse", "--verify", outgoing[0]+"^")
	if err != nil {
		return "", fmt.Errorf("%.12s is a root commit, which GitHub's API can't create", outgoing[0])
	}
	repo := &Repository{path: "."}
	changes, err := repo.Changes(outgoing...)
	if err != nil {
		return "", err
	}
	tree, err := git("", "rev-parse", local+"^{tree}")
	if err != nil {
		return "", err
	}
	token, err := credentialFor(h.url)
	if err != nil {
		return "", err
	}
	branch := strings.TrimPrefix(dst, "refs/heads/")
	newHead, err := headlessPush(context.Background(), token, h.target, branch, base, old == "", force, tree, changes)
	if err != nil {
		return "", fmt.Errorf("GitHub API push failed, nothing changed locally: %w", err)
	}

	// Move the pushed local branch onto the signed commits. They have the same tree, so the index
	// and working tree stay as they are. Passing the old value never moves a ref changed meanwhile.
	note := "run `git pull --rebase` to get them"
	ref, _ := git("", "rev-parse", "--symbolic-full-name", src)
	if strings.HasPrefix(ref, "refs/heads/") || ref == "HEAD" {
		if repo.FetchFrom(h.url, dst) == nil {
			if _, err := git("", "update-ref", "-m", "push: signed by GitHub", ref, newHead, local); err == nil {
				note = "moved " + strings.TrimPrefix(ref, "refs/heads/") + " onto them (same files)"
			}
		}
	}
	fmt.Fprintf(os.Stderr, "headless: GitHub signed %d commit(s) on %s, %.7s -> %.7s; %s\n", len(outgoing), branch, local, newHead, note)
	return newHead, nil
}

// nativePush pushes a refspec with regular git. An explicit pushurl stops pushInsteadOf from
// routing it back to this helper.
func (h *remoteHelper) nativePush(src, dst string, force bool) error {
	args := []string{"-c", "remote.headless-native.url=" + h.url, "-c", "remote.headless-native.pushurl=" + h.url, "push", "--quiet"}
	if lease, ok := h.leases[dst]; ok {
		args = append(args, "--force-with-lease="+dst+":"+lease)
	} else if force {
		args = append(args, "--force")
	}
	_, err := git("", append(args, "headless-native", src+":"+dst)...)
	return err
}

func isSigned(commit string) bool {
	raw, _ := git("", "cat-file", "commit", commit)
	headers, _, _ := strings.Cut(raw, "\n\n")
	return strings.Contains(headers, "\ngpgsig")
}

// parseGitHubURL extracts owner/repo from https://github.com/owner/repo(.git).
func parseGitHubURL(raw string) (targetFlag, error) {
	path, ok := strings.CutPrefix(raw, "https://github.com/")
	owner, repo, _ := strings.Cut(strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git"), "/")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		// Don't echo the URL: it may embed a token.
		return "", errors.New("the remote URL is not https://github.com/<owner>/<repo>; check `git remote -v`")
	}
	return targetFlag(owner + "/" + repo), nil
}

// credentialFor returns the token git's credential helpers hold for the remote URL, so pushes
// use the same identity as fetches.
var credentialFor = func(remoteURL string) (string, error) {
	out, _ := git("url="+remoteURL+"\n\n", "-c", "core.askPass=true", "credential", "fill")
	for _, line := range strings.Split(out, "\n") {
		if password, ok := strings.CutPrefix(line, "password="); ok && password != "" {
			return password, nil
		}
	}
	return "", errors.New("no credentials from `git credential fill`; configure a credential helper for https://github.com")
}

// git runs git with stdin, returning its trimmed output or an error carrying its first stderr line.
func git(stdin string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return "", fmt.Errorf("git %s: %s", args[0], first)
	}
	return strings.TrimSpace(string(out)), nil
}
