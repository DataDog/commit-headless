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
    echo "commits=\"$(git log "${{ github.sha }}".. --format='%H%x00' | tr '\n' ' ')\"" >> $GITHUB_OUTPUT

- name: Push commits
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    token: ${{ github.token }} # default
    target: ${{ github.repository }} # default
    branch: ${{ github.ref_name }}
    command: push
    commits: "${{ steps.create-commits.outputs.commits }}"
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
  uses: DataDog/commit-headless@action/v%%VERSION%%
  with:
    token: ${{ github.token }} # default
    target: ${{ github.repository }} # default
    branch: ${{ github.ref_name }}
    author: "A U Thor <author@example.com>" # defaults to the github-actions bot account
    message: "a commit message"
    command: commit
    files: "${{ steps.create-commits.outputs.files }}"
    force: true # default false, needs to be true to allow deletion
```
