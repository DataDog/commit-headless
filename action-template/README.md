# commit-headless action

NOTE: This branch contains only the action implementation of `commit-headless`. To view the source
code, see the [main](https://github.com/DataDog/commit-headless/tree/main) branch.

This action uses `commit-headless` to support creating signed and verified remote commits from a
GitHub action workflow.

For more details on how `commit-headless` works, check the main branch link above.

## Usage (commit-headless push)

The `push` command automatically determines which local commits need to be pushed by comparing
local HEAD with the remote branch HEAD.

```
- name: Create commits
  run: |
    git config --global user.name "A U Thor"
    git config --global user.email "author@example.com"

    echo "new file from my bot" >> bot.txt
    git add bot.txt && git commit -m"bot commit 1"

    echo "another commit" >> bot.txt
    git add bot.txt && git commit -m"bot commit 2"

- name: Push commits
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    token: ${{ github.token }} # default
    target: ${{ github.repository }} # default
    branch: ${{ github.ref_name }}
    command: push
```

If you primarily create commits on *new* branches, you'll want to use the `create-branch` option. This
example creates a commit with the current time in a file, and then pushes it to a branch named
`build-timestamp`, creating it from the current commit hash if the branch doesn't exist.

```
- name: Create commits
  run: |
    git config --global user.name "A U Thor"
    git config --global user.email "author@example.com"

    echo "BUILD-TIMESTAMP-RFC3339: $(date --rfc-3339=s)" > last-build.txt
    git add last-build.txt && git commit -m"update build timestamp"

- name: Push commits
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: build-timestamp
    head-sha: ${{ github.sha }}
    create-branch: true
    command: push
```

## Usage (commit-headless commit)

The `commit` command creates a single commit from staged changes, similar to `git commit`. Stage
your changes with `git add`, then run the action.

Unlike `push`, the `commit` command does not require any relationship between local and remote
history. This makes it useful for broadcasting the same file changes to multiple repositories.

```
- name: Make and stage changes
  run: |
    echo "updating contents of bot.txt" >> bot.txt
    date --rfc-3339=s >> timestamp
    git add bot.txt timestamp

    # Deletions work too
    git rm -f old-file.txt || true

- name: Create commit
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: ${{ github.ref_name }}
    author: "A U Thor <author@example.com>" # defaults to the github-actions bot account
    message: "a commit message"
    command: commit
```

### Broadcasting to multiple repositories

The `commit` command can apply the same staged changes to multiple repositories, even if they have
unrelated histories:

```
- name: Stage shared configuration
  run: |
    git add config.yml security-policy.md

- name: Update repo1
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    target: org/repo1
    branch: main
    message: "Update security policy"
    command: commit

- name: Update repo2
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    target: org/repo2
    branch: main
    message: "Update security policy"
    command: commit
```

## Usage (commit-headless replay)

The `replay` command replays existing remote commits as signed commits. This is useful when an
earlier step in your workflow creates unsigned commits and you want to replace them with signed
versions.

```
- name: Some action that creates unsigned commits
  uses: some-org/some-action@v1
  # This action creates commits but they're not signed

- name: Replay commits as signed
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: ${{ github.ref_name }}
    since: ${{ github.sha }}  # The commit before the unsigned commits
    command: replay
```

The `since` input specifies the base commit (exclusive) - all commits after this point will be
replayed as signed commits. The branch is then force-updated to point to the new signed commits.

**Warning:** This command force-pushes to the remote branch.
