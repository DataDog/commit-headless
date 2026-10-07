package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// When git runs this test binary as git-remote-headless, act as the helper with a local bare
// repository standing in for GitHub.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "git-remote-headless" {
		bare := os.Args[2]
		headlessPush = func(_ context.Context, _ targetFlag, branch, base string, _, _ bool, changes []Change) (string, error) {
			return fakeGitHubPush(bare, branch, base, changes)
		}
		logger = NewLogger(io.Discard)
		h := &remoteHelper{repo: &Repository{path: "."}, remote: os.Args[1], url: bare, target: "owner/repo", out: os.Stdout, stderr: os.Stderr}
		if err := h.serve(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeGitHubPush recreates each change in bare with a different committer, like GitHub does.
func fakeGitHubPush(bare, branch, head string, changes []Change) (string, error) {
	run := func(gitDir string, args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"--git-dir", gitDir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=A U Thor", "GIT_AUTHOR_EMAIL=author@home.arpa", "GIT_COMMITTER_NAME=GitHub", "GIT_COMMITTER_EMAIL=noreply@github.com", "GIT_COMMITTER_DATE=1700000000 +0000")
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	for _, c := range changes {
		if out, err := run(os.Getenv("GIT_DIR"), "push", "--quiet", "--no-verify", bare, c.hash+":refs/fake/objects"); err != nil {
			return "", fmt.Errorf("%s", out)
		}
		next, err := run(bare, "commit-tree", c.hash+"^{tree}", "-p", head, "-m", c.message)
		if err != nil {
			return "", fmt.Errorf("%s", next)
		}
		head = next
	}
	if out, err := run(bare, "update-ref", "refs/heads/"+branch, head); err != nil {
		return "", fmt.Errorf("%s", out)
	}
	_, _ = run(bare, "update-ref", "-d", "refs/fake/objects")
	return head, nil
}

type helperFixture struct {
	*testRepository
	bare string
}

func newHelperFixture(t *testing.T) *helperFixture {
	t.Helper()
	bin := t.TempDir()
	self, err := os.Executable()
	requireNoError(t, err)
	requireNoError(t, os.Symlink(self, filepath.Join(bin, "git-remote-headless")))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	f := &helperFixture{testRepository: &testRepository{t: t}, bare: filepath.Join(t.TempDir(), "remote.git")}
	f.init()
	requireNoError(t, exec.Command("git", "init", "--quiet", "--bare", f.bare).Run())
	f.git("remote", "add", "origin", f.bare)
	f.commit("base")
	f.git("branch", "-M", "main")
	f.git("push", "--quiet", "origin", "main") // seed before enabling the helper
	f.git("fetch", "--quiet", "origin")
	f.git("config", "url.headless::"+f.bare+".pushInsteadOf", f.bare)
	return f
}

func (f *helperFixture) commit(name string) string {
	requireNoError(f.t, os.WriteFile(filepath.Join(f.root, name), []byte(name+"\n"), 0o644))
	f.git("add", name)
	f.git("commit", "--quiet", "-m", name)
	return f.rev("HEAD")
}

func (f *helperFixture) rev(ref string) string {
	return strings.TrimSpace(string(f.git("rev-parse", ref)))
}

func (f *helperFixture) remoteRev(ref string) string {
	out, _ := exec.Command("git", "--git-dir", f.bare, "rev-parse", "--verify", "--quiet", ref).Output()
	return strings.TrimSpace(string(out))
}

// push runs git push and returns its combined output and whether it succeeded.
func (f *helperFixture) push(args ...string) (string, bool) {
	cmd := exec.Command("git", append([]string{"push"}, args...)...)
	cmd.Dir = f.root
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func TestRemoteHelperRewritesUnsignedCommits(t *testing.T) {
	f := newHelperFixture(t)
	f.git("checkout", "--quiet", "-b", "feature")
	f.commit("one")
	local := f.commit("two")
	requireNoError(t, os.WriteFile(filepath.Join(f.root, "one"), []byte("uncommitted\n"), 0o644))

	out, ok := f.push("-u", "origin", "feature")
	if !ok {
		t.Fatalf("push failed:\n%s", out)
	}
	if !strings.Contains(out, "headless: GitHub signed 2 commit(s) on feature") || !strings.Contains(out, "feature rewritten to match") {
		t.Errorf("expected a one-line rewrite notice, got:\n%s", out)
	}

	signed := f.remoteRev("refs/heads/feature")
	if signed == "" || signed == local {
		t.Fatalf("expected remote feature to hold rewritten commits, got %q (local was %s)", signed, local)
	}
	if got := f.rev("feature"); got != signed {
		t.Errorf("local feature = %s, want %s", got, signed)
	}
	if got := f.rev("origin/feature"); got != signed {
		t.Errorf("origin/feature = %s, want %s", got, signed)
	}
	if got := string(f.git("status", "--porcelain")); strings.TrimSpace(got) != "M one" {
		t.Errorf("uncommitted change should survive untouched, status:\n%s", got)
	}
	if got := strings.TrimSpace(string(f.git("rev-parse", "--abbrev-ref", "feature@{upstream}"))); got != "origin/feature" {
		t.Errorf("upstream = %q, want origin/feature", got)
	}

	// A follow-up fast-forward push only rewrites the new commit.
	f.git("checkout", "--quiet", "one")
	f.commit("three")
	out, ok = f.push()
	if !ok || !strings.Contains(out, "signed 1 commit(s)") {
		t.Fatalf("follow-up push:\n%s", out)
	}
	if f.rev("feature") != f.remoteRev("refs/heads/feature") {
		t.Errorf("local and remote diverged after follow-up push")
	}

	out, ok = f.push()
	if !ok || !strings.Contains(out, "Everything up-to-date") {
		t.Errorf("second push should be a no-op:\n%s", out)
	}
}

func TestRemoteHelperOnlyRewritesCommitsMissingFromTheRemote(t *testing.T) {
	f := newHelperFixture(t)
	// Someone else moves main after our fetch: our origin/main is no longer a remote tip.
	later := exec.Command("git", "--git-dir", f.bare, "commit-tree", "main^{tree}", "-p", "main", "-m", "later")
	later.Env = append(os.Environ(), "GIT_AUTHOR_NAME=x", "GIT_AUTHOR_EMAIL=x@x", "GIT_COMMITTER_NAME=x", "GIT_COMMITTER_EMAIL=x@x")
	sha, err := later.Output()
	requireNoError(t, err)
	requireNoError(t, exec.Command("git", "--git-dir", f.bare, "update-ref", "refs/heads/main", strings.TrimSpace(string(sha))).Run())

	f.git("checkout", "--quiet", "-b", "feature")
	f.commit("mine")
	out, ok := f.push("origin", "feature")
	if !ok || !strings.Contains(out, "signed 1 commit(s)") {
		t.Fatalf("only the local commit should be rewritten:\n%s", out)
	}
}

func TestRemoteHelperForceWithLease(t *testing.T) {
	f := newHelperFixture(t)
	f.git("checkout", "--quiet", "-b", "feature")
	f.commit("one")
	if out, ok := f.push("-u", "origin", "feature"); !ok {
		t.Fatalf("push failed:\n%s", out)
	}
	f.git("commit", "--quiet", "--amend", "-m", "one, amended")

	if out, ok := f.push("--force-with-lease=feature:" + strings.Repeat("0", 40)); ok || !strings.Contains(out, "stale info") {
		t.Errorf("a wrong lease must be rejected:\n%s", out)
	}
	out, ok := f.push("--force-with-lease")
	if !ok || !strings.Contains(out, "signed 1 commit(s)") {
		t.Fatalf("force-with-lease push:\n%s", out)
	}
	if f.rev("feature") != f.remoteRev("refs/heads/feature") {
		t.Errorf("local and remote differ after force push")
	}
}

func TestRemoteHelperRefusesWhatTheAPICannotSign(t *testing.T) {
	for name, setup := range map[string]func(f *helperFixture){
		"merge commit": func(f *helperFixture) {
			f.git("checkout", "--quiet", "-b", "side")
			f.commit("side")
			f.git("checkout", "--quiet", "feature")
			f.commit("main-line")
			f.git("merge", "--quiet", "--no-edit", "side")
		},
		"executable": func(f *helperFixture) {
			requireNoError(t, os.WriteFile(filepath.Join(f.root, "run.sh"), []byte("#!/bin/sh\n"), 0o755))
			f.git("add", "run.sh")
			f.git("commit", "--quiet", "-m", "script")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHelperFixture(t)
			f.git("checkout", "--quiet", "-b", "feature")
			setup(f)
			local := f.rev("feature")

			out, ok := f.push("origin", "feature")
			if ok || !strings.Contains(out, "sign the commits locally instead") || !strings.Contains(out, remoteHelperDocs) {
				t.Errorf("expected an actionable rejection, got:\n%s", out)
			}
			if f.remoteRev("refs/heads/feature") != "" || f.rev("feature") != local {
				t.Errorf("a rejected push must change nothing")
			}
		})
	}
}

func TestRemoteHelperAllowsEditingAnExecutable(t *testing.T) {
	f := newHelperFixture(t)
	f.git("checkout", "--quiet", "-b", "feature")
	requireNoError(t, os.WriteFile(filepath.Join(f.root, "run.sh"), []byte("#!/bin/sh\n"), 0o755))
	f.git("add", "run.sh")
	f.git("commit", "--quiet", "-m", "script")
	// Publish the script without the helper, as if it had been signed elsewhere.
	requireNoError(t, exec.Command("git", "--git-dir", f.bare, "fetch", "--quiet", f.root, "feature:feature").Run())
	f.git("fetch", "--quiet", "origin")

	// GitHub keeps the mode of a file whose content changes (verified live).
	requireNoError(t, os.WriteFile(filepath.Join(f.root, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755))
	f.git("commit", "--quiet", "-am", "edit script")
	if out, ok := f.push("origin", "feature"); !ok || !strings.Contains(out, "signed 1 commit(s)") {
		t.Fatalf("editing an executable should be signed:\n%s", out)
	}
}

func TestRemoteHelperPassesThroughWhatNeedsNoSigning(t *testing.T) {
	f := newHelperFixture(t)

	// Already-signed commits keep their SHA. The signature is fake: only its presence matters.
	raw := string(f.git("cat-file", "commit", f.commit("signed")))
	withSig := strings.Replace(raw, "\n\n", "\ngpgsig -----BEGIN SSH SIGNATURE-----\n -----END SSH SIGNATURE-----\n\n", 1)
	cmd := exec.Command("git", "hash-object", "-t", "commit", "-w", "--stdin")
	cmd.Dir, cmd.Stdin = f.root, strings.NewReader(withSig)
	sha, err := cmd.Output()
	requireNoError(t, err)
	signed := strings.TrimSpace(string(sha))
	f.git("reset", "--quiet", "--hard", signed)

	for _, args := range [][]string{
		{"origin", "HEAD:refs/heads/signed"},
		{"origin", "main:refs/heads/copy"},
		{"origin", "main:refs/tags/v1"},
		{"origin", ":refs/heads/copy"},
	} {
		if out, ok := f.push(args...); !ok || strings.Contains(out, "headless: ") {
			t.Errorf("git push %v should pass through untouched:\n%s", args, out)
		}
	}
	if f.remoteRev("refs/heads/signed") != signed || f.remoteRev("refs/tags/v1") == "" || f.remoteRev("refs/heads/copy") != "" {
		t.Errorf("pass-through pushes did not land as expected")
	}
}

func TestParseGitHubURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/DataDog/commit-headless":     "DataDog/commit-headless",
		"https://github.com/DataDog/commit-headless.git": "DataDog/commit-headless",
		"https://github.com/DataDog/commit-headless/":    "DataDog/commit-headless",
		"git@github.com:DataDog/commit-headless.git":     "",
		"https://gitlab.com/DataDog/commit-headless":     "",
		"https://github.com/DataDog":                     "",
	} {
		got, err := parseGitHubURL(raw)
		if string(got) != want || (err == nil) != (want != "") {
			t.Errorf("parseGitHubURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}
