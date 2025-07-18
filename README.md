# commit-headless

A binary tool and GitHub Action for creating signed commits from headless workflows

For the Action, please see [the action branch][action-branch] and the associated `action/`
release tags.

`commit-headless` is focused on turning local commits (or dirty files) into signed commits on the
remote. It does this via the GitHub GraphQL API, more specifically the [createCommitOnBranch][mutation]
mutation.

When this API is used with a GitHub App token, the resulting commit will be signed and verified by
GitHub on behalf of the application.

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

Note that, by default, both of these commands expect the remote branch to already exist. If your
workflow primarily works on *new* branches, you should additionally add the `--branch-from` flag and
supply a commit hash to use as a branch point. With this flag, `commit-headless` will create the
branch on GitHub from that commit hash if it doesn't already exist.

Example: `commit-headless <command> [flags...] --branch-from=$(git rev-parse main HEAD) ...`

In normal usage, `commit-headless` will print *only* the reference to the last commit created on the
remote, allowing this to easily be captured in a script.

More on the specifics for each command below. See also: `commit-headless <command> --help`

### commit-headless push

In addition to the required target and branch flags, the `push` command expects a list of commit
hashes as arguments *or* a list of commit hashes *in reverse chronological order (newest first)*
on standard input.

It will iterate over the supplied commits, extract the set of changed files and commit message, then
craft new *remote* commits corresponding to each local commit.

The remote commit will have the original commit message, with "Co-authored-by" trailer for the
original commit author.

You can use `commit-headless push` via:

    commit-headless push [flags...] HASH1 HASH2 HASH3 ...

Or, using git log (note `--oneline`):

    git log --oneline main.. | commit-headless push [flags...]

### commit-headless commit

This command is more geared for creating single commits at a time. It takes a list of files to
commit changes to, and those files will either be updated/added or deleted in a single commit.

Note that you cannot delete a file without also adding `--force` for safety reasons.

Examples:

    # Commit changes to these two files
    commit-headless commit [flags...] -- README.md .gitlab-ci.yml

    # Remove a file, add another one, and commit
    rm file/i/do/not/want
    echo "hello" > hi-there.txt
    commit-headless commit [flags...] --force -- hi-there.txt file/i/do/not/want

    # Commit a change with a custom message
    commit-headless commit [flags...] -m"ran a pipeline" -- output.txt

## Try it!

You can try `commit-headless` locally. The resulting commits will be authored and committed by you.
The commits on `bot-branch-remote` in this repository were entirely created via this tool based on
local commits created like so:

    git commit --no-gpg-sign --author='Bot <bot@mailinator.com>'

## Example output

The below output was generated from `commit-headless` running on some local commits to the
`bot-branch-remote` branch.

All output other than the final commit hash printed at the end is written to stderr, and can be
redirected to a file.

```sh
Owner: datadog
Repository: commit-headless
Branch: bot-branch-remote
Commits: 7e94985, 89c7296, b89e749, 9a1a616
Current head commit: 84485a25ea7cac03d42eb1571d4d46974ade837b
Commit 7e94985979a76a9ef72248007c118dc565bc5715
  Headline: bot: update README.md
  Changed files: 1
    - MODIFY: README.md
Commit 89c7296eafeefb6165edf0b27e8b287f4695724e
  Headline: bot: add botfile.txt
  Changed files: 1
    - MODIFY: botfile.txt
Commit b89e7494601c5f001bf923386edc4e9cf7d8ec76
  Headline: bot: remove botfile.txt
  Changed files: 1
    - DELETE: botfile.txt
Commit 9a1a616c80c44b228e2890b811490a40beb198b9
  Headline: bot: rename README.md -> README.markdown
  Changed files: 2
    - DELETE: README.md
    - MODIFY: README.markdown
Pushed 4 commits.
Branch URL: https://github.com/datadog/commit-headless/commits/bot-branch-remote
281ff0fa1204e93c8931a774c6ebe2c69e66eddd
```

## Releasing

The release process has two parts to it: prerelease and publish.

Prerelease occurs automatically on a push to main, or can be manually triggered by the
`release:build` job on any branch.

Additionally, on main, the `release:publish` job will run. This job takes the prerelease image and
tags it for release, as well as produces a CI image with various other tools.

You can view all releases (and prereleases) with crane:

```
$ crane ls registry.ddbuild.io/commit-headless-prerelease
$ crane ls registry.ddbuild.io/commit-headless
$ crane ls registry.ddbuild.io/commit-headless-ci-image
```

Note that the final publish job will fail unless there was also a change to `version.go` to avoid
overwriting existing releases.
