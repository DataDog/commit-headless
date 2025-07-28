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

Usage example:

    # Commit changes to these two files
    commit-headless commit [flags...] -- README.md .gitlab-ci.yml

    # Remove a file, add another one, and commit
    rm file/i/do/not/want
    echo "hello" > hi-there.txt
    commit-headless commit [flags...] --force -- hi-there.txt file/i/do/not/want

    # Commit a change with a custom message
    commit-headless commit [flags...] -m"ran a pipeline" -- output.txt

## Try it!

You can easily try `commit-headless` locally. Create a commit with a different author (to
demonstrate how commit-headless attributes changes to the original author), and run it with a GitHub
token.

For example, create a commit locally and push it to a new branch using the current branch as the
branch point:

```
cd ~/Code/repo
echo "bot commit here" >> README.md
git add README.md
git commit --author='A U Thor <author@example.com>' --message="test bot commit"
HEADLESS_TOKEN=$(ddtool auth github token) commit-headless push \
    --target=owner/repo \
    --branch=bot-branch \
    --branch-from="$(git rev-parse HEAD^)" \ # use the previous commit as our branch point
    "$(git rev-parse HEAD)" # push the commit we just created
```

## Examples

- This repository uses the action to [release itself][usage-action].
- DataDog/service-discovery-platform uses it to [update bazel dependencies][usage-service-disco].
- DataDog/web-ui, DataDog/profiling-backend, and DataDog/dogweb all use it [for the weekly staging reset][usage-staging-reset].
- DataDog/web-ui uses it for the [Automated packages lint fix][usage-web-ui-lint] PR commits.
- DataDog/cloud-tf-ci uses it for [updating the terraform CI image][usage-cloud-tf-ci].
- DataDog/k8s-platform-resources uses it [to bump Chart versions][usage-k8s-p-r].
- DataDog/datadog-vscode uses the action to [replicate README changes into the public repository][usage-vscode].
- DataDog/websites-astro uses the action to [update some site content][usage-websites-astro].

[usage-action]: /.github/workflows/release.yml
[usage-service-disco]: https://github.com/DataDog/service-discovery-platform/pull/10615
[usage-staging-reset]: https://github.com/DataDog/dogweb/pull/145992
[usage-web-ui-lint]:  https://github.com/DataDog/web-ui/pull/219111
[usage-cloud-tf-ci]: https://github.com/DataDog/cloud-tf-ci/pull/556
[usage-k8s-p-r]: https://github.com/DataDog/k8s-platform-resources/pull/16307
[usage-vscode]: https://github.com/DataDog/datadog-vscode/blob/main/.github/actions/readme/action.yaml
[usage-websites-astro]: https://github.com/DataDog/websites-astro/blob/main/.github/workflows/update-content.yml

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

## Releasing the Action

The action release is simlar to the above process, although driven by a GitHub Workflow (see
`.github/workflows/release.yml`). When a change is made to the default branch, the contents of
`action-template/` are used to create a new commit on the `action` branch.

Because the workflow uses the rendered action (and the built binary) to create the commit to the
action branch we are fairly safe from releasing a broken version of the action.

Assuming the previous step works, the workflow will then create a tag of the form `action/vVERSION`.
