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
		headlessPush = func(_ context.Context, _ string, _ targetFlag, branch, base string, _, _ bool, _ string, changes []Change) (string, error) {
			return fakeGitHubPush(bare, branch, base, changes)
		}
		credentialFor = func(string) (string, error) { return "ghu_test", nil }
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
		if out, err := run(os.Getenv("GIT_DIR"), "push", "--quiet", bare, c.hash+":refs/fake/objects"); err != nil {
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
	if got := f.rev("origin/feature"); got != signed && gitReportsNewOid() {
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

func TestRemoteHelperIgnoresStaleTrackingRefs(t *testing.T) {
	f := newHelperFixture(t)
	f.git("checkout", "--quiet", "-b", "feature")
	f.commit("one")
	if out, ok := f.push("-u", "origin", "feature"); !ok {
		t.Fatalf("push failed:\n%s", out)
	}
	// Someone force-pushes feature back to main; our origin/feature still points at "one".
	requireNoError(t, exec.Command("git", "--git-dir", f.bare, "update-ref", "refs/heads/feature", "refs/heads/main").Run())
	f.commit("two")

	out, ok := f.push("--force", "origin", "feature")
	if !ok || !strings.Contains(out, "signed 2 commit(s)") {
		t.Fatalf("both commits missing from the remote must be signed:\n%s", out)
	}
}

func TestRemoteHelperBatchEdgeCases(t *testing.T) {
	t.Run("one commit to two branches is signed once", func(t *testing.T) {
		f := newHelperFixture(t)
		f.git("checkout", "--quiet", "-b", "feature")
		f.commit("one")
		if out, ok := f.push("origin", "feature:a", "feature:b"); !ok || strings.Count(out, "signed 1 commit(s)") != 1 {
			t.Fatalf("expected a single rewrite:\n%s", out)
		}
		signed := f.rev("feature")
		for _, ref := range []string{"refs/heads/a", "refs/heads/b"} {
			if got := f.remoteRev(ref); got != signed {
				t.Errorf("%s = %s, want %s", ref, got, signed)
			}
		}
		if gitReportsNewOid() && (f.rev("origin/a") != signed || f.rev("origin/b") != signed) {
			t.Errorf("tracking refs must record the signed commit")
		}
	})

	t.Run("lease that the branch must not exist", func(t *testing.T) {
		f := newHelperFixture(t)
		f.git("checkout", "--quiet", "-b", "feature")
		f.commit("one")
		if out, ok := f.push("--force-with-lease=refs/heads/fresh:", "origin", "feature:fresh"); !ok {
			t.Fatalf("an absent branch satisfies an empty lease:\n%s", out)
		}
	})

	t.Run("directory replaced by a regular file", func(t *testing.T) {
		f := newHelperFixture(t)
		requireNoError(t, os.MkdirAll(filepath.Join(f.root, "x"), 0o755))
		requireNoError(t, os.WriteFile(filepath.Join(f.root, "x", "y"), []byte("y\n"), 0o644))
		f.git("add", "x")
		f.git("commit", "--quiet", "-m", "dir")
		requireNoError(t, exec.Command("git", "--git-dir", f.bare, "fetch", "--quiet", f.root, "main:main").Run())
		f.git("fetch", "--quiet", "origin")
		f.git("checkout", "--quiet", "-b", "feature")
		f.git("rm", "--quiet", "-r", "x")
		requireNoError(t, os.WriteFile(filepath.Join(f.root, "x"), []byte("now a file\n"), 0o644))
		f.git("add", "x")
		f.git("commit", "--quiet", "-m", "file")
		if out, ok := f.push("origin", "feature"); !ok || !strings.Contains(out, "signed 1 commit(s)") {
			t.Fatalf("a new regular file replacing a directory should be signed:\n%s", out)
		}
	})
}

func TestRemoteHelperForceWithLease(t *testing.T) {
	f := newHelperFixture(t)
	f.git("checkout", "--quiet", "-b", "feature")
	f.commit("one")
	if out, ok := f.push("-u", "origin", "feature"); !ok {
		t.Fatalf("push failed:\n%s", out)
	}
	if !gitReportsNewOid() {
		f.git("fetch", "--quiet", "origin") // as the push notice asks on git < 2.29
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
		"new executable": func(f *helperFixture) {
			requireNoError(t, os.WriteFile(filepath.Join(f.root, "run.sh"), []byte("#!/bin/sh\n"), 0o755))
			f.git("add", "run.sh")
			f.git("commit", "--quiet", "-m", "script")
		},
		"mode change": func(f *helperFixture) {
			f.git("update-index", "--chmod=+x", "base")
			f.git("commit", "--quiet", "-m", "make base executable")
		},
		"root commit": func(f *helperFixture) {
			f.git("checkout", "--quiet", "--orphan", "fresh")
			f.git("commit", "--quiet", "-m", "root")
			f.git("branch", "-f", "feature", "fresh")
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newHelperFixture(t)
			f.git("checkout", "--quiet", "-b", "feature")
			setup(f)
			local := f.rev("feature")

			out, ok := f.push("origin", "feature")
			if ok || !strings.Contains(out, "sign the commits locally") || !strings.Contains(out, remoteHelperDocs) {
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
