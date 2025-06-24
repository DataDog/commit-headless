# commit-headless

A binary tool and GitHub action for creating signed commits from headless workflows

`commit-headless` is focused on turning local commits (or dirty files) into signed commits on the
remote. It does this via the GitHub GraphQL API, more specifically the [createCommitOnBranch][mutation]
mutation.

When this API is used with a GitHub App token, the resulting commit will be signed and verified by
GitHub on behalf of the application.

[mutation]: https://docs.github.com/en/graphql/reference/mutations#createcommitonbranch)

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

You can use `commit-headless push` via:

    GH_TOKEN=xyz commit-headless push -R datadog/commit-headless -b bot-branch-remote HASH1 HASH2 HASH3 ...

Or, using git log (note `--oneline`):

    git log --oneline main.. | GH_TOKEN=xyz commit-headless push -R datadog/commit-headless -b bot-branch-remote

## Try it!

You can try `commit-headless` locally. The resulting commits will be authored and committed by you.
The commits on `bot-branch-remote` in this repository were entirely created via this tool based on
local commits created like so:

    git commit --no-gpg-sign --author='Bot <bot@mailinator.com>'
