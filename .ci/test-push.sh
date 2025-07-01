#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# test-push.sh
# Tests the in-development version of commit-headless by creating a new branch with a new commit,
# then pushing that new commit with commit-headless and asserting that it completed without error.

TREE=$(mktemp -d)
AUTHOR="Gitlab CI <${CI_PIPELINE_ID}@gitlab.ddbuild.io>"
BRANCH="test-pipeline/${CI_PIPELINE_ID}"

# First, build commit-headless and store the binary somewhere we can use it
go build -o /tmp/commit-headless-dev -buildvcs=false .

# Copied from default.before_script
git config --global --add safe.directory $PWD

# Create a detached worktree in our working directory, switch to it, and create an orphaned branch
git worktree add -d "${TREE}"

pushd "${TREE}"

git switch --orphan "${BRANCH}"

source "${GITLAB_ENV}"

# Add github as a remote
git remote add github "https://anyuser:${STS_TOKEN}@github.com/DataDog/commit-headless.git"

# Create and push an initial commit, which will create the branch on the GitHub side
git commit --author="${AUTHOR}" --allow-empty -m"initial commit"

git push --set-upstream github "${BRANCH}"

# Create a commit
echo "pipeline: ${CI_PIPELINE_ID}" >> pipeline.txt
git add pipeline.txt
git commit --author="${AUTHOR}" -m"commit from commit-headless ${CI_COMMIT_SHORT_SHA}"

# Get the revision for this commit
REV=$(git rev-parse HEAD)

# And use commit-headless-dev to push it to the remote branch
/tmp/commit-headless-dev push -R DataDog/commit-headless --branch "${BRANCH}" "${REV}"

# Finally, delete the branch on the GitHub side

popd
