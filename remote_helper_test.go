package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// When git runs this test binary as git-remote-headless, it serves as the helper, with a bare
// repository standing in for GitHub.
func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "git-remote-headless" {
		headlessPush = fakeGitHubPush(os.Args[2])
		credentialFor = func(string) (string, error) { return "ghu_test", nil }
		if err := (&remoteHelper{remote: os.Args[1], url: os.Args[2], target: "owner/repo", out: os.Stdout}).serve(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeGitHubPush recreates each change in bare with GitHub as the committer.
func fakeGitHubPush(bare string) func(context.Context, string, targetFlag, string, string, bool, bool, string, []Change) (string, error) {
	return func(_ context.Context, _ string, _ targetFlag, branch, head string, _, _ bool, _ string, changes []Change) (string, error) {
		git := func(args ...string) (string, error) {
			cmd := exec.Command("git", append([]string{"--git-dir", bare}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_COMMITTER_NAME=GitHub", "GIT_COMMITTER_EMAIL=noreply@github.com")
			out, err := cmd.CombinedOutput()
			return strings.TrimSpace(string(out)), err
		}
		for _, c := range changes {
			next, err := git("commit-tree", c.hash+"^{tree}", "-p", head, "-m", c.message)
			if err != nil {
				return "", fmt.Errorf("%s", next)
			}
			head = next
		}
		_, err := git("update-ref", "refs/heads/"+branch, head)
		return head, err
	}
}

type helperRepo struct {
	*testRepository
	bare string
}

// newHelperRepo returns a repository on branch feature, whose origin is a bare repository with a
// main branch, and whose pushes go through git-remote-headless.
func newHelperRepo(t *testing.T) *helperRepo {
	var major, minor int
	out, _ := exec.Command("git", "version").Output()
	fmt.Sscanf(string(out), "git version %d.%d", &major, &minor)
	if major == 2 && minor < 29 {
		t.Skip("git-remote-headless needs git 2.29+ (option new-oid)")
	}

	bin := t.TempDir()
	requireNoError(t, os.Symlink(os.Args[0], filepath.Join(bin, "git-remote-headless")))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	for _, v := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(v, "x@home.arpa")
	}

	r := &helperRepo{testRepository: testRepo(t), bare: filepath.Join(t.TempDir(), "remote.git")}
	requireNoError(t, exec.Command("git", "init", "--quiet", "--bare", r.bare).Run())
	// The fake GitHub reads local objects directly, as if they had been uploaded.
	requireNoError(t, os.WriteFile(filepath.Join(r.bare, "objects", "info", "alternates"), []byte(r.path(".git", "objects")), 0o644))
	r.git("remote", "add", "origin", r.bare)
	r.commit("base")
	r.git("branch", "-M", "main")
	r.git("push", "--quiet", "-u", "origin", "main")
	r.git("config", "url.headless::"+r.bare+".pushInsteadOf", r.bare)
	r.git("checkout", "--quiet", "-b", "feature")
	return r
}

func (r *helperRepo) commit(name string) string {
	requireNoError(r.t, os.WriteFile(r.path(name), []byte(name), 0o644))
	r.git("add", name)
	r.git("commit", "--quiet", "-m", name)
	return r.rev("HEAD")
}

// signedCommit commits name with a (fake) signature: only its presence matters to the helper.
func (r *helperRepo) signedCommit(name string) string {
	raw := string(r.git("cat-file", "commit", r.commit(name)))
	cmd := exec.Command("git", "hash-object", "-t", "commit", "-w", "--stdin")
	cmd.Dir, cmd.Stdin = r.root, strings.NewReader(strings.Replace(raw, "\n\n", "\ngpgsig fake\n\n", 1))
	out, err := cmd.Output()
	requireNoError(r.t, err)
	r.git("reset", "--quiet", "--soft", strings.TrimSpace(string(out)))
	return r.rev("HEAD")
}

func (r *helperRepo) rev(ref string) string {
	return strings.TrimSpace(string(r.git("rev-parse", ref)))
}

func (r *helperRepo) remoteRev(ref string) string {
	out, _ := exec.Command("git", "--git-dir", r.bare, "rev-parse", "--verify", "--quiet", ref).Output()
	return strings.TrimSpace(string(out))
}

func (r *helperRepo) push(args ...string) string {
	cmd := exec.Command("git", append([]string{"push"}, args...)...)
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "FAILED: " + string(out)
	}
	return string(out)
}

func TestRemoteHelperSignsAndMovesTheBranch(t *testing.T) {
	r := newHelperRepo(t)
	r.commit("one")
	unsigned := r.commit("two")
	requireNoError(t, os.WriteFile(r.path("one"), []byte("uncommitted"), 0o644))

	if out := r.push("-u", "origin", "feature"); !strings.Contains(out, "headless: GitHub signed 2 commit(s) on feature") {
		t.Fatalf("push: %s", out)
	}
	signed := r.remoteRev("feature")
	if signed == unsigned || r.rev("feature") != signed || r.rev("origin/feature") != signed {
		t.Errorf("feature, origin/feature and the remote must all be on the signed copy %s", signed)
	}
	if status := strings.TrimSpace(string(r.git("status", "--porcelain"))); status != "M one" {
		t.Errorf("uncommitted changes must survive, status: %q", status)
	}

	r.commit("three")
	if out := r.push(); !strings.Contains(out, "signed 1 commit(s)") {
		t.Errorf("follow-up push must only sign the new commit: %s", out)
	}
	if out := r.push(); !strings.Contains(out, "Everything up-to-date") {
		t.Errorf("repeated push must be a no-op: %s", out)
	}
}

func TestRemoteHelperKeepsSignedCommits(t *testing.T) {
	r := newHelperRepo(t)
	signed := r.signedCommit("signed")
	if out := r.push("origin", "feature"); strings.Contains(out, "GitHub signed") || r.remoteRev("feature") != signed {
		t.Fatalf("signed commits must be pushed as they are: %s", out)
	}

	r.signedCommit("signed again")
	r.commit("unsigned")
	if out := r.push("origin", "feature"); !strings.Contains(out, "signed 1 commit(s)") || r.rev("feature^") != r.remoteRev("feature^") {
		t.Errorf("only the unsigned commit must be recreated: %s", out)
	}
}

func TestRemoteHelperSkipsCommitsTheRemoteHas(t *testing.T) {
	r := newHelperRepo(t)
	// main moves on the remote after our last fetch, so origin/main is no longer a remote tip.
	newer, err := exec.Command("git", "--git-dir", r.bare, "commit-tree", "main^{tree}", "-p", "main", "-m", "newer").Output()
	requireNoError(t, err)
	requireNoError(t, exec.Command("git", "--git-dir", r.bare, "update-ref", "refs/heads/main", strings.TrimSpace(string(newer))).Run())

	r.commit("mine")
	if out := r.push("origin", "feature"); !strings.Contains(out, "signed 1 commit(s)") {
		t.Errorf("only the local commit must be signed: %s", out)
	}
}

func TestRemoteHelperForceWithLease(t *testing.T) {
	r := newHelperRepo(t)
	r.commit("one")
	r.push("-u", "origin", "feature")
	r.git("commit", "--quiet", "--amend", "-m", "amended")

	if out := r.push("--force-with-lease=feature:" + strings.Repeat("1", 40)); !strings.Contains(out, "stale info") {
		t.Errorf("a wrong lease must be rejected: %s", out)
	}
	if out := r.push("--force-with-lease"); !strings.Contains(out, "signed 1 commit(s)") || r.rev("feature") != r.remoteRev("feature") {
		t.Errorf("force-with-lease push: %s", out)
	}
}

func TestRemoteHelperPassesThroughWhatNeedsNoNewCommits(t *testing.T) {
	r := newHelperRepo(t)
	for _, args := range [][]string{{"origin", "main:copy"}, {"origin", "main:refs/tags/v1"}, {"origin", "--delete", "copy"}} {
		if out := r.push(args...); strings.Contains(out, "FAILED") || strings.Contains(out, "GitHub signed") {
			t.Errorf("git push %v must go through regular git: %s", args, out)
		}
	}
	if r.remoteRev("refs/tags/v1") == "" || r.remoteRev("copy") != "" {
		t.Errorf("pass-through pushes did not land")
	}
}

func TestRemoteHelperRefusesMergeCommits(t *testing.T) {
	r := newHelperRepo(t)
	r.commit("one")
	r.git("checkout", "--quiet", "-b", "side", "main")
	r.commit("side")
	r.git("checkout", "--quiet", "feature")
	r.git("merge", "--quiet", "--no-edit", "side")

	if out := r.push("origin", "feature"); !strings.Contains(out, "FAILED") || !strings.Contains(out, "merge commit") {
		t.Errorf("merge commits must be refused: %s", out)
	}
	if r.remoteRev("feature") != "" {
		t.Errorf("a refused push must not create the branch")
	}
}

func TestParseGitHubURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://github.com/DataDog/commit-headless":     "DataDog/commit-headless",
		"https://github.com/DataDog/commit-headless.git": "DataDog/commit-headless",
		"https://github.com/DataDog/commit-headless/":    "DataDog/commit-headless",
		"git@github.com:DataDog/commit-headless.git":     "",
		"https://github.com/DataDog":                     "",
		"https://x:ghp_secret@github.com/o/r":            "",
	} {
		got, err := parseGitHubURL(raw)
		if string(got) != want || (err == nil) != (want != "") || (err != nil && strings.Contains(err.Error(), "secret")) {
			t.Errorf("parseGitHubURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
}
