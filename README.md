# commit-headless

A binary tool and GitHub action for creating signed commits from headless workflows

`commit-headless` is focused on turning local commits (or dirty files) into signed commits on the
remote. It does this via the GitHub GraphQL API, more specifically the [createCommitOnBranch][mutation]
mutation.

When this API is used with a GitHub App token, the resulting commit will be signed and verified by
GitHub on behalf of the application.

[mutation]: https://docs.github.com/en/graphql/reference/mutations#createcommitonbranch

## Usage

Currently, there is one command: `commit-headless push`. It takes a target owner/repository and
remote branch name, as well as a list of commit hashes as arguments *or* a list of commit hashes *in
reverse chronological order (newest first)* on standard input.

It will iterate over the supplied commits, extract the set of changed files and commit message, then
craft new *remote* commits corresponding to each local commit.

The remote commit will have the original commit message, with "Co-authored-by" trailer for the
original commit message. This is because commits created using the GraphQL API do not support
setting the author or committer (they are inferred from the token owner), so adding a
"Co-authored-by" trailer allows the commits to carry attribution to the original (bot) committer.

In normal usage, `commit-headless` will print *only* the reference to the last commit created on the
remote, allowing this to easily be captured in a script. For example output, see the later section.

You can use `commit-headless push` via:

    GH_TOKEN=xyz commit-headless push --target datadog/commit-headless --branch bot-branch-remote HASH1 HASH2 HASH3 ...

Or, using git log (note `--oneline`):

    git log --oneline main.. | GH_TOKEN=xyz commit-headless push --target datadog/commit-headless --branch bot-branch-remote

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

Releases are automated by merging a change to VERSION.

Additionally, a prerelease can be triggered on any branch by manually running the release job.

Generally speaking, releases and prereleases are the same. They both run a series of live tests
against this repository to assert that commit-headless can do its job.
