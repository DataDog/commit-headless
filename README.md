# commit-headless action

NOTE: This branch contains only the action implementation of `commit-headless`. To view the source
code, see the [main](https://github.com/DataDog/commit-headless/tree/main) branch.

This action uses `commit-headless` to support creating signed and verified remote commits from a
GitHub action workflow.

For more details on how `commit-headless` works, check the main branch link above.

## Usage (commit-headless push)

If your workflow creates multiple commits and you want to push all of them, you can use
`commit-headless push`:

```
- name: Create commits
  id: create-commits
  run: |
    git config --global user.name "A U Thor"
    git config --global user.email "author@example.com"

    echo "new file from my bot" >> bot.txt
    git add bot.txt && git commit -m"bot commit 1"

    echo "another commit" >> bot.txt
    git add bot.txt && git commit -m"bot commit 2"

    # List both commit hashes in reverse order, space separated
    echo "commits=\"$(git log "${{ github.sha }}".. --format='%H' | tr '\n' ' ')\"" >> $GITHUB_OUTPUT

    # If you just have a single commit, you can do something like:
    #  echo "commit=$(git rev-parse HEAD)" >> $GITHUB_OUTPUT
    # and then use it in the action via:
    #  with:
    #    ...
    #    commits: ${{ steps.create-commits.outputs.commit }}

- name: Push commits
  uses: DataDog/commit-headless@action/v1.0.0
  with:
    token: ${{ github.token }} # default
    target: ${{ github.repository }} # default
    branch: ${{ github.ref_name }}
    command: push
    commits: "${{ steps.create-commits.outputs.commits }}"
```

If you primarily create commits on *new* branches, you'll want to use the `branch-from` option. This
example creates a commit with the current time in a file, and then pushes it to a branch named
`build-timestamp`, creating it from the current commit hash if the branch doesn't exist.

```
- name: Create commits
  id: create-commits
  run: |
    git config --global user.name "A U Thor"
    git config --global user.email "author@example.com"

    echo "BUILD-TIMESTAMP-RFC3339: $(date --rfc-3339=s)" > last-build.txt
    git add last-build.txt && git commit -m"update build timestamp"

    # Store the created commit as a step output
    echo "commit=$(git rev-parse HEAD)" >> $GITHUB_OUTPUT

- name: Push commits
  uses: DataDog/commit-headless@action/v1.0.0
  with:
    branch: build-timestamp
    branch-from: ${{ github.sha }}
    command: push
    commits: "${{ steps.create-commits.outputs.commit }}"
```

## Usage (commit-headless commit)

Some workflows may just have a specific set of files that they change and just want to create a
single commit out of them. For that, you can use `commit-headless commit`:

```
- name: Change files
  id: change-files
  run: |
    echo "updating contents of bot.txt" >> bot.txt

    date --rfc-3339=s >> timestamp

    files="bot.txt timestamp"

    # remove an old file if it exists
    # commit-headless commit will fail if you attempt to delete a file that doesn't exist on the
    # remote (enforced via the GitHub API)
    if [[ -f timestamp.old ]]; then
        rm timestamp.old
        files += " timestamp.old"
    fi

    # Record the set of files we want to commit
    echo "files=\"${files}\"" >> $GITHUB_OUTPUT

- name: Create commit
  uses: DataDog/commit-headless@action/v1.0.0
  with:
    branch: ${{ github.ref_name }}
    author: "A U Thor <author@example.com>" # defaults to the github-actions bot account
    message: "a commit message"
    command: commit
    files: "${{ steps.create-commits.outputs.files }}"
    force: true # default false, needs to be true to allow deletion
```
