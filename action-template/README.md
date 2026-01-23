# commit-headless action

This action creates signed and verified commits on GitHub from a workflow.

For source code and CLI documentation, see the [main branch](https://github.com/DataDog/commit-headless/tree/main).

## Commands

- [push](#push) - Push local commits as signed commits
- [commit](#commit) - Create a signed commit from staged changes
- [replay](#replay) - Re-sign existing remote commits

## Inputs

| Input | Description | Required | Default |
|-------|-------------|----------|---------|
| `command` | Command to run: `push`, `commit`, or `replay` | Yes | |
| `branch` | Target branch name | Yes | |
| `token` | GitHub token | No | `${{ github.token }}` |
| `target` | Target repository (owner/repo) | No | `${{ github.repository }}` |
| `head-sha` | Expected HEAD SHA (safety check) or branch point | No | |
| `create-branch` | Create the branch if it doesn't exist | No | `false` |
| `dry-run` | Skip actual remote writes | No | `false` |
| `message` | Commit message (for `commit` command) | No | |
| `author` | Commit author (for `commit` command) | No | github-actions bot |
| `since` | Base commit to replay from (for `replay` command) | No | |
| `working-directory` | Directory to run in | No | |

## Outputs

| Output | Description |
|--------|-------------|
| `pushed_ref` | SHA of the last commit created |

## push

Push local commits to the remote as signed commits.

```yaml
- name: Create commits
  run: |
    git config user.name "A U Thor"
    git config user.email "author@example.com"

    echo "new file from my bot" >> bot.txt
    git add bot.txt && git commit -m "bot commit 1"

    echo "another commit" >> bot.txt
    git add bot.txt && git commit -m "bot commit 2"

- name: Push commits
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: ${{ github.ref_name }}
    command: push
```

### Creating a new branch

Use `create-branch` with `head-sha` to create the branch if it doesn't exist:

```yaml
- name: Create commits
  run: |
    git config user.name "A U Thor"
    git config user.email "author@example.com"

    echo "BUILD-TIMESTAMP: $(date --rfc-3339=s)" > last-build.txt
    git add last-build.txt && git commit -m "update build timestamp"

- name: Push commits
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: build-timestamp
    head-sha: ${{ github.sha }}
    create-branch: true
    command: push
```

## commit

Create a signed commit from staged changes. Unlike `push`, this doesn't require any relationship
between local and remote history.

```yaml
- name: Stage changes
  run: |
    echo "updating bot.txt" >> bot.txt
    date --rfc-3339=s >> timestamp
    git add bot.txt timestamp

    # Deletions work too
    git rm -f old-file.txt || true

- name: Create commit
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: ${{ github.ref_name }}
    author: "A U Thor <author@example.com>"
    message: "a commit message"
    command: commit
```

### Broadcasting to multiple repositories

Apply the same staged changes to multiple repositories:

```yaml
- name: Stage shared configuration
  run: git add config.yml security-policy.md

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

## replay

Re-sign existing remote commits. Useful when an earlier step creates unsigned commits.

```yaml
- name: Some action that creates unsigned commits
  uses: some-org/some-action@v1

- name: Replay commits as signed
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    branch: ${{ github.ref_name }}
    since: ${{ github.sha }}
    command: replay
```

The `since` input specifies the base commit (exclusive). All commits after this point are replayed
as signed commits, and the branch is force-updated.

**Warning:** This command force-pushes to the remote branch.
