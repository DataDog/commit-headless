# commit-headless

A binary tool and GitHub Action for creating signed commits from headless workflows.

`commit-headless` turns local commits into commits on the remote using the GitHub API.
By default, commits are created using the GraphQL `createCommitOnBranch` mutation, which produces
signed and verified commits regardless of token type. For commits that modify files with non-default
modes (e.g., executables) and a non-user token, the tool automatically falls back to the REST API
to preserve file modes.

File modes (such as the executable bit) are preserved when using the REST API path. The GraphQL API
does not support file modes — all files are treated as regular files (`100644`). For user tokens
(PAT, OAuth, `ghu_`), signing takes priority and the GraphQL path is always used.

For the GitHub Action, see [the action branch][action-branch] and the associated `action/` release
tags.

[action-branch]: https://github.com/DataDog/commit-headless/tree/action

## Commands

- [push](#push) - Push local commits to the remote as signed commits
- [commit](#commit) - Create a signed commit from staged changes
- [replay](#replay) - Re-sign existing remote commits

All commands require:
- A target repository: `--target/-T owner/repo`
- A branch name: `--branch branch-name`
- A GitHub token in one of: `HEADLESS_TOKEN`, `GITHUB_TOKEN`, or `GH_TOKEN`

On success, `commit-headless` prints only the SHA of the last commit created, allowing easy capture
in scripts.

## push

The `push` command automatically determines which local commits need to be pushed by comparing
local HEAD with the remote branch HEAD. It extracts the changed files and commit message from each
local commit and creates corresponding signed commits on the remote.

The remote commits will have the original commit message, with a "Co-authored-by" trailer for the
original commit author.

Basic usage:

    # Push local commits to an existing remote branch
    commit-headless push -T owner/repo --branch feature

    # Push with a safety check that remote HEAD matches expected value
    commit-headless push -T owner/repo --branch feature --head-sha abc123

    # Create a new branch and push local commits to it
    commit-headless push -T owner/repo --branch new-feature --head-sha abc123 --create-branch

### Safety check with --head-sha

By default, `commit-headless` queries the GitHub API to get the current HEAD of the remote branch.
This introduces risk on active branches: if a new commit is pushed after your job starts, your
push will overwrite those changes.

Specifying `--head-sha` adds a safety check: the push fails if the remote HEAD doesn't match the
expected value.

### Creating a new branch

By default, the target branch must already exist. To create a new branch, use `--create-branch`
with `--head-sha` specifying the branch point:

    commit-headless push -T owner/repo --branch new-feature --head-sha abc123 --create-branch

### Force-pushing after rebase

When a branch has been rebased, the local history diverges from the remote and a normal push will
fail. Use `--force` with `--head-sha` to push the rebased commits:

    # After rebasing onto updated main:
    commit-headless push -T owner/repo --branch feature \
        --head-sha "$(git rev-parse main)" --force

The `--head-sha` value is used as the parent of the first pushed commit, bypassing the remote HEAD
check. The branch ref is force-updated even though the push is not a fast-forward.

`--force` requires `--head-sha` to be set.

### Diverged history

The remote HEAD (or `--head-sha` if `--create-branch` or `--force` is set) must be an ancestor of
local HEAD. If it isn't, the push fails to prevent creating broken history.

## commit

The `commit` command creates a single signed commit on the remote from your currently staged
changes, similar to `git commit`. Stage your changes with `git add`, then run this command.

Staged deletions (`git rm`) are also supported. The staged file paths must match the paths on the
remote.

Basic usage:

    # Stage changes and commit to remote
    git add README.md
    commit-headless commit -T owner/repo --branch feature -m "Update docs"

    # Stage a deletion and a new file
    git rm old-file.txt
    git add new-file.txt
    commit-headless commit -T owner/repo --branch feature -m "Replace old with new"

### Broadcasting to multiple repositories

Unlike `push`, the `commit` command does not require any relationship between local and remote
history. This makes it useful for applying the same changes to multiple repositories:

    git add config.yml security-policy.md
    commit-headless commit -T org/repo1 --branch main -m "Update security policy"
    commit-headless commit -T org/repo2 --branch main -m "Update security policy"
    commit-headless commit -T org/repo3 --branch main -m "Update security policy"

## replay

The `replay` command re-signs existing remote commits. This is useful when you have unsigned
commits on a branch (e.g., from a bot or action that doesn't support signed commits) and want to
replace them with signed versions.

The command fetches the remote branch, extracts commits since the specified base, recreates them
as signed commits, and force-updates the branch ref.

Basic usage:

    # Replay all commits since abc123 as signed commits
    commit-headless replay -T owner/repo --branch feature --since abc123

    # With safety check that remote HEAD matches expected value
    commit-headless replay -T owner/repo --branch feature --since abc123 --head-sha def456

**Warning:** This command force-pushes to the remote branch. The `--since` commit must be an
ancestor of the branch HEAD.

## Signature verification

By default, `commit-headless` verifies that each commit created via the API is signed by GitHub.
If a commit is not signed, it retries with exponential backoff (1s, 2s, 4s, ...) up to
`--sign-attempts` times (default: 5).

All token types can produce signed commits. The GraphQL API (used by default) produces signed
commits for both user tokens and app tokens. The REST API fallback also produces signed commits
for GitHub App / installation tokens.

Even with a valid token, GitHub may occasionally fail to sign a commit. This has been observed
internally and is not consistently reproducible. The retry mechanism exists as a safety net for
these transient failures.

If all attempts are exhausted without a signed commit, `commit-headless` exits with an error. This
ensures unsigned commits are never silently pushed to the remote.

## API strategy

`commit-headless` automatically chooses between the GraphQL and REST APIs on a per-commit basis:

- **GraphQL** (default): Uses `createCommitOnBranch` — fewer API calls, commits are signed
  server-side. Does not support file modes (all files are `100644`).
- **REST** (fallback): Uses the Git Database API — more API calls, but preserves file modes.

The REST fallback is used when a commit modifies files with non-default modes (e.g., `100755` for
executables) and the token is not a user token. User tokens (`ghp_`, `gho_`, `ghu_`,
`github_pat_`) always use GraphQL since signing is the priority.

The tool logs which strategy is used for each commit. If the GraphQL path is used for a commit with
non-default file modes, a warning is logged.

## Try it

Create a local commit and push it to a new branch:

```
cd ~/Code/repo
echo "bot commit here" >> README.md
git add README.md
git commit --author='A U Thor <author@example.com>' -m "test bot commit"

commit-headless push \
    -T owner/repo \
    --branch bot-branch \
    --head-sha "$(git rev-parse HEAD^)" \
    --create-branch \
    --sign-attempts 0
```

The `--head-sha "$(git rev-parse HEAD^)"` tells commit-headless to create the branch from the
parent of your new commit, so only your new commit gets pushed.

Or push to an existing branch:

```
commit-headless push -T owner/repo --branch existing-branch --sign-attempts 0
```

