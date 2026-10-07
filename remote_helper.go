package main

// git-remote-headless is a git remote helper (see gitremote-helpers(7)) that makes a plain
// `git push` land GitHub-signed commits, for machines without a signing key. Enable it with:
//
//	ln -s commit-headless git-remote-headless   # anywhere on PATH
//	git config --global url.headless::https://github.com/.pushInsteadOf https://github.com/
//
// Fetches are untouched. On push, unsigned commits are recreated through the GitHub API (which
// signs them), then the local branch is moved onto the signed commits. Their trees are identical,
// so the index and working tree are unaffected. Everything that needs no signing (signed commits,
// deletions, tags) is passed through to a regular git push.

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const remoteHelperDocs = "https://github.com/DataDog/commit-headless#git-remote-headless"

// headlessPush creates signed copies of changes on top of base and points branch at them,
// returning the new head. It's a variable so tests can replace GitHub.
var headlessPush = func(ctx context.Context, target targetFlag, branch, base string, create, force bool, changes []Change) (string, error) {
	token, err := credentialFor(target)
	if err != nil {
		return "", err
	}
	client := NewClient(ctx, token, target.Owner(), target.Repository(), branch)
	client.force = force
	client.signAttempts = 5
	if create {
		if _, err := client.CreateBranch(ctx, base); err != nil {
			return "", err
		}
	}
	_, head, err := client.PushChanges(ctx, base, changes...)
	return head, err
}

type pushSpec struct {
	src, dst string
	force    bool
}

type remoteHelper struct {
	repo       *Repository
	url        string
	target     targetFlag
	out        io.Writer
	stderr     io.Writer
	remoteRefs map[string]string
	leases     map[string]string
	dryRun     bool
}

// remoteHelperMain is the entry point when invoked as git-remote-headless <remote> <url>.
func remoteHelperMain(args []string) int {
	if len(args) != 2 {
		fmt.Fprintf(os.Stderr, "git-remote-headless is invoked by git, see %s\n", remoteHelperDocs)
		return 1
	}
	target, err := parseGitHubURL(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "headless: %s (see %s)\n", err, remoteHelperDocs)
		return 1
	}
	logger = NewLogger(io.Discard)
	h := &remoteHelper{repo: &Repository{path: "."}, url: args[1], target: target, out: os.Stdout, stderr: os.Stderr}
	if err := h.serve(os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "headless: %s\n", err)
		return 1
	}
	return 0
}

// parseGitHubURL extracts owner/repo from https://github.com/owner/repo(.git).
func parseGitHubURL(raw string) (targetFlag, error) {
	u, err := url.Parse(raw)
	parts := []string{}
	if err == nil {
		parts = strings.Split(strings.Trim(u.Path, "/"), "/")
	}
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || len(parts) != 2 {
		return "", fmt.Errorf("%q is not an https://github.com/<owner>/<repo> URL", raw)
	}
	return targetFlag(parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")), nil
}

// serve speaks the remote helper protocol until git closes stdin or sends a blank line.
func (h *remoteHelper) serve(in io.Reader) error {
	scanner := bufio.NewScanner(in)
	var batch []pushSpec
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "capabilities":
			fmt.Fprint(h.out, "push\noption\n\n")
		case strings.HasPrefix(line, "option "):
			fmt.Fprintln(h.out, h.option(strings.TrimPrefix(line, "option ")))
		case line == "list" || line == "list for-push":
			if err := h.list(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "push "):
			src, dst, _ := strings.Cut(strings.TrimPrefix(line, "push "), ":")
			batch = append(batch, pushSpec{src: strings.TrimPrefix(src, "+"), dst: dst, force: strings.HasPrefix(src, "+")})
		case line == "" && batch != nil:
			for _, spec := range batch {
				fmt.Fprint(h.out, h.push(spec))
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

func (h *remoteHelper) option(opt string) string {
	name, value, _ := strings.Cut(opt, " ")
	switch name {
	case "verbosity":
		if n, _ := strconv.Atoi(value); n > 1 {
			logger = NewLogger(h.stderr)
		}
	case "progress":
	case "dry-run":
		h.dryRun = value == "true"
	case "cas":
		// --force-with-lease: "<ref>:<expected sha>", empty sha meaning "must not exist"
		ref, expected, _ := strings.Cut(value, ":")
		if h.leases == nil {
			h.leases = map[string]string{}
		}
		h.leases[ref] = expected
	default:
		return "unsupported"
	}
	return "ok"
}

// list prints the remote branches and tags, which git uses for up-to-date and fast-forward checks.
func (h *remoteHelper) list() error {
	cmd := exec.Command("git", "ls-remote", h.url, "refs/heads/*", "refs/tags/*")
	cmd.Stderr = h.stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not list %s; check that `git ls-remote %s` works (credentials, network): %w", h.url, h.url, err)
	}
	h.remoteRefs = map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		sha, ref, ok := strings.Cut(line, "\t")
		if ok && !strings.HasSuffix(ref, "^{}") {
			h.remoteRefs[ref] = sha
			fmt.Fprintf(h.out, "%s %s\n", sha, ref)
		}
	}
	fmt.Fprintln(h.out)
	return nil
}

// push handles one refspec and returns its protocol status lines.
func (h *remoteHelper) push(spec pushSpec) string {
	newHead, err := h.pushOne(spec)
	if err != nil {
		fmt.Fprintf(h.stderr, "headless: %s: %s\n", spec.dst, err)
		return fmt.Sprintf("error %s %s\n", spec.dst, quoteStatus(err.Error()))
	}
	if newHead == "" {
		return fmt.Sprintf("ok %s\n", spec.dst)
	}
	// Tell git the remote branch now points at the signed commit, so it records that in the
	// remote-tracking ref instead of the local commit it pushed.
	return fmt.Sprintf("ok %s\noption new-oid %s\n", spec.dst, newHead)
}

// pushOne returns the new remote head when commits were rewritten, or "" when the refspec was
// pushed as is.
func (h *remoteHelper) pushOne(spec pushSpec) (string, error) {
	old := h.remoteRefs[spec.dst]
	if expected, ok := h.leases[spec.dst]; ok && expected != old {
		return "", fmt.Errorf("stale info")
	}
	if spec.src == "" || !strings.HasPrefix(spec.dst, "refs/heads/") {
		return "", h.nativePush(spec)
	}

	local, err := h.git("rev-parse", "--verify", spec.src+"^{commit}")
	if err != nil {
		return "", err
	}
	outgoing, err := h.outgoing(local)
	if err != nil {
		return "", err
	}
	unsigned, err := h.anyUnsigned(outgoing)
	if err != nil {
		return "", err
	}
	if !unsigned {
		return "", h.nativePush(spec)
	}

	base, changes, err := h.headlessChanges(outgoing)
	if err != nil {
		return "", err
	}
	branch := strings.TrimPrefix(spec.dst, "refs/heads/")
	if h.dryRun {
		return "", nil
	}
	newHead, err := headlessPush(context.Background(), h.target, branch, base, old == "", spec.force, changes)
	if err != nil {
		return "", fmt.Errorf("GitHub API push failed, nothing was rewritten locally: %w (see %s)", err, remoteHelperDocs)
	}
	return newHead, h.adoptSigned(spec, local, newHead, len(outgoing))
}

// outgoing lists the commits reachable from local but from no remote ref, oldest first.
func (h *remoteHelper) outgoing(local string) ([]string, error) {
	known := &strings.Builder{}
	for _, sha := range h.remoteRefs {
		// Remote commits we never fetched can't be in local history; rev-list rejects them.
		if _, err := h.git("cat-file", "-e", sha+"^{commit}"); err == nil {
			fmt.Fprintf(known, "^%s\n", sha)
		}
	}
	cmd := exec.Command("git", "rev-list", "--reverse", "--stdin", local)
	cmd.Dir = h.repo.path
	cmd.Stdin = strings.NewReader(known.String())
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rev-list: %w", err)
	}
	return strings.Fields(string(out)), nil
}

func (h *remoteHelper) anyUnsigned(commits []string) (bool, error) {
	for _, c := range commits {
		raw, err := h.git("cat-file", "commit", c)
		if err != nil {
			return false, err
		}
		headers, _, _ := strings.Cut(raw, "\n\n")
		if !strings.Contains(headers, "\ngpgsig") {
			return true, nil
		}
	}
	return false, nil
}

// headlessChanges checks the outgoing commits can be recreated faithfully through the API and
// returns their base and contents.
func (h *remoteHelper) headlessChanges(outgoing []string) (string, []Change, error) {
	signLocally := fmt.Sprintf("sign the commits locally instead (git rebase --exec 'git commit --amend --no-edit -S' <base>) and push again, see %s", remoteHelperDocs)
	base := ""
	for i, c := range outgoing {
		parents, err := h.git("rev-list", "--parents", "-n1", c)
		if err != nil {
			return "", nil, err
		}
		fields := strings.Fields(parents)
		if len(fields) != 2 {
			return "", nil, fmt.Errorf("%.12s is a merge or root commit, which GitHub's API can't sign; rebase instead of merging, or %s", c, signLocally)
		}
		if i == 0 {
			base = fields[1]
		}
	}
	changes, err := h.repo.Changes(outgoing...)
	if err != nil {
		return "", nil, err
	}
	for _, change := range changes {
		for path, fe := range change.entries {
			if fe.Content != nil && fe.Mode != "100644" {
				return "", nil, fmt.Errorf("%.12s changes %s (mode %s); GitHub's API only signs regular files, %s", change.hash, path, fe.Mode, signLocally)
			}
		}
	}
	return base, changes, nil
}

// adoptSigned fetches the signed commits and moves the pushed local branch onto them.
func (h *remoteHelper) adoptSigned(spec pushSpec, local, newHead string, count int) error {
	if _, err := h.git("fetch", "--quiet", "--no-tags", "--no-write-fetch-head", h.url, spec.dst); err != nil {
		return fmt.Errorf("pushed %s but could not fetch it: %w; run `git pull --rebase` to sync", newHead, err)
	}
	localTree, _ := h.git("rev-parse", local+"^{tree}")
	signedTree, err := h.git("rev-parse", newHead+"^{tree}")
	if err != nil || signedTree != localTree {
		return fmt.Errorf("pushed %s but its content differs from local %.12s; inspect with `git diff %.12s %.12s` (please report: %s)", newHead, local, local, newHead, remoteHelperDocs)
	}

	ref, _ := h.git("rev-parse", "--symbolic-full-name", spec.src)
	moved := "local commits left unchanged"
	if strings.HasPrefix(ref, "refs/heads/") || ref == "HEAD" {
		// Compare-and-swap: never move a ref that changed while we were pushing.
		if _, err := h.git("update-ref", "--no-deref", "-m", "push: re-signed by GitHub", ref, newHead, local); err == nil {
			moved = fmt.Sprintf("%s rewritten to match (same files)", strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	fmt.Fprintf(h.stderr, "headless: GitHub signed %d commit(s) on %s, %.7s -> %.7s; %s\n",
		count, strings.TrimPrefix(spec.dst, "refs/heads/"), local, newHead, moved)
	return nil
}

// nativePush pushes a refspec with regular git. An explicit pushurl stops git from applying
// pushInsteadOf, which would otherwise route this push back to us.
func (h *remoteHelper) nativePush(spec pushSpec) error {
	args := []string{"-c", "remote.headless-native.url=" + h.url, "-c", "remote.headless-native.pushurl=" + h.url,
		"push", "--quiet", "--no-verify"}
	if h.dryRun {
		args = append(args, "--dry-run")
	}
	if expected, ok := h.leases[spec.dst]; ok {
		args = append(args, "--force-with-lease="+spec.dst+":"+expected)
	} else if spec.force {
		args = append(args, "--force")
	}
	_, err := h.git(append(args, "headless-native", spec.src+":"+spec.dst)...)
	return err
}

func (h *remoteHelper) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = h.repo.path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

// credentialFor returns HEADLESS_TOKEN or the password git's credential helpers hold for the
// repository, so pushes use the same identity as fetches.
func credentialFor(target targetFlag) (string, error) {
	if token := os.Getenv("HEADLESS_TOKEN"); token != "" {
		return token, nil
	}
	cmd := exec.Command("git", "credential", "fill")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("protocol=https\nhost=github.com\npath=%s.git\n\n", target))
	out, _ := cmd.Output()
	for _, line := range strings.Split(string(out), "\n") {
		if password, ok := strings.CutPrefix(line, "password="); ok && password != "" {
			return password, nil
		}
	}
	return "", fmt.Errorf("no GitHub token for %s: set HEADLESS_TOKEN or configure a git credential helper for https://github.com", target)
}

// quoteStatus makes a message safe for a single protocol line.
func quoteStatus(msg string) string {
	return fmt.Sprintf("%q", strings.ReplaceAll(msg, "\n", " "))
}
