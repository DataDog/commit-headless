# commit-headless

A binary tool and GitHub Action for creating signed commits from headless workflows

For the Action, please see [the action branch][action-branch] and the associated `action/`
release tags. For example usage, see [Examples](#examples).

`commit-headless` is focused on turning local commits into signed commits on the remote. It does
this using the GitHub API, more specifically the [createCommitOnBranch][mutation] mutation. When
commits are created using the API (instead of via `git push`), the commits will be signed and
verified by GitHub on behalf of the owner of the credentials used to access the API.


*NOTE:* One limitation of creating commits using the GraphQL API is that it does not expose any
mechanism to set or change file modes. It merely takes the file contents, base64 encoded. This means
that if you rely on `commit-headless` to push binary files (or executable scripts), the file in the
resulting commit will not retain that executable bit.

[mutation]: https://docs.github.com/en/graphql/reference/mutations#createcommitonbranch
[action-branch]: https://github.com/DataDog/commit-headless/tree/action

## Usage

There are two ways to create signed headless commits with this tool: `push` and `commit`.

Both of these commands take a target owner/repository (eg, `--target/-T DataDog/commit-headless`)
and remote branch name (eg, `--branch bot-branch`) as required flags and expect to find a GitHub
token in one of the following environment variables:

- HEADLESS_TOKEN
- GITHUB_TOKEN
- GH_TOKEN

In normal usage, `commit-headless` will print *only* the reference to the last commit created on the
remote, allowing this to easily be captured in a script.

More on the specifics for each command below. See also: `commit-headless <command> --help`

### Specifying the expected head commit

When creating remote commits via API, `commit-headless` must specify the "expected head sha" of the
remote branch. By default, `commit-headless` will query the GitHub API to get the *current* HEAD
commit of the remote branch and use that as the "expected head sha". This introduces some risk,
especially for active branches or long running jobs, as a new commit introduced after the job starts
will not be considered when pushing the new commits. The commit itself will not be replaced, but the
changes it introduces may be lost.

For example, consider an auto-formatting job. It runs `gofmt` over the entire codebase. If the job
starts on commit A and formats a file `main.go`, and while the job is running the branch gains
commit B, which adds *new* changes to `main.go`, when the lint job finishes the formatted version of
`main.go` from commit A will be pushed to the remote, and overwrite the changes to `main.go`
introduced in commit B.

You can avoid this by specifying `--head-sha`. This will skip auto discovery of the remote branch
HEAD and instead require that the remote branch HEAD matches the value of `--head-sha`. If the
remote branch HEAD does not match `--head-sha`, the push will fail (which is likely what you want).

### Creating a new branch

Note that, by default, both of these commands expect the remote branch to already exist. If your
workflow primarily works on *new* branches, you should additionally add the `--create-branch` flag
and supply a commit hash to use as a branch point via `--head-sha`. With this flag,
`commit-headless` will create the branch on GitHub from that commit hash if it doesn't already
exist.

Example: `commit-headless <command> [flags...] --head-sha=$(git rev-parse main HEAD) --create-branch ...`

### commit-headless push

The `push` command automatically determines which local commits need to be pushed by comparing
local HEAD with the remote branch HEAD. It then iterates over those commits, extracts the changed
files and commit message, and creates corresponding remote commits.

The remote commits will have the original commit message, with a "Co-authored-by" trailer for the
original commit author.

Basic usage:

    # Push local commits to an existing remote branch
    commit-headless push -T owner/repo --branch feature

    # Push with a safety check that remote HEAD matches expected value
    commit-headless push -T owner/repo --branch feature --head-sha abc123

    # Create a new branch and push local commits to it
    commit-headless push -T owner/repo --branch new-feature --head-sha abc123 --create-branch

**Note:** The remote HEAD (or `--head-sha` when creating a branch) must be an ancestor of local
HEAD. If the histories have diverged, the push will fail with an error. This ensures you don't
accidentally create broken history when the local checkout is out of sync with the remote.

### commit-headless commit

The `commit` command creates a single commit on the remote from the currently staged changes,
similar to how `git commit` works. Stage your changes first with `git add`, then run this command
to push them as a signed commit on the remote.

The staged file paths must match the paths on the remote. That is, if you stage "path/to/file.txt"
then the contents of that file will be applied to that same path on the remote.

Staged deletions (`git rm`) are also supported.

Unlike `push`, the `commit` command does not require any relationship between local and remote
history. This makes it useful for broadcasting the same file changes to multiple repositories,
even if they have completely unrelated histories:

    # Apply the same changes to multiple repositories
    git add config.yml security-policy.md
    commit-headless commit -T org/repo1 --branch main -m "Update security policy"
    commit-headless commit -T org/repo2 --branch main -m "Update security policy"
    commit-headless commit -T org/repo3 --branch main -m "Update security policy"

Basic usage:

    # Stage changes and commit to remote
    git add README.md .gitlab-ci.yml
    commit-headless commit -T owner/repo --branch feature -m "Update docs"

    # Stage a deletion and a new file
    git rm old-file.txt
    git add new-file.txt
    commit-headless commit -T owner/repo --branch feature -m "Replace old with new"

    # Stage all changes and commit
    git add -A
    commit-headless commit -T owner/repo --branch feature -m "Update everything"

## Try it!

You can easily try `commit-headless` locally. Create a commit with a different author (to
demonstrate how commit-headless attributes changes to the original author), and run it with a GitHub
token.

For example, create a commit locally and push it to a new branch using the parent commit as the
branch point:

```
cd ~/Code/repo
echo "bot commit here" >> README.md
git add README.md
git commit --author='A U Thor <author@example.com>' --message="test bot commit"
# Assuming a github token in $GITHUB_TOKEN or $HEADLESS_TOKEN
commit-headless push \
    --target=owner/repo \
    --branch=bot-branch \
    --head-sha="$(git rev-parse HEAD^)" \
    --create-branch
```

Or, to push to an existing branch:

```
commit-headless push --target=owner/repo --branch=existing-branch
```

## Action Releases

On a merge to main, if there's not already a tagged release for the current version (in
`version.go`), a new tag will be created on the action branch.

The action branch contains prebuilt binaries of `commit-headless` to avoid having to use Docker
based (composite) actions, or to avoid having to download the binary when the action runs.

Because the workflow uses the rendered action (and the built binary) to create the commit to the
action branch we are fairly safe from releasing a broken version of the action.

Assuming the previous step works, the workflow will then create a tag of the form `action/vVERSION`.

For more on the action release, see the [workflow](.github/workflows/release.yml).

## Internal Image Releases

See the internal commit-headless-ci-config repository.
