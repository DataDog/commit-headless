#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Print commands, but do not expand variables (avoid leaking secrets)
set -o verbose

# test-push.sh
# Tests the in-development version of commit-headless by creating a new branch with a new commit,
# then pushing that new commit with commit-headless and asserting that it completed without error.

BRANCH="test-pipeline/${CI_PIPELINE_ID}"
ORIGINAL_NAME="$(git config user.name || echo '')"
ORIGINAL_EMAIL="$(git config user.email || echo '')"

# First, build commit-headless and store the binary somewhere we can use it
go build -o /tmp/commit-headless-dev -buildvcs=false .

# Copied from default.before_script
git config --global --add safe.directory $PWD

git config user.name "Gitlab CI"
git config user.email "${CI_PIPELINE_ID}@gitlab.ddbuild.io"

git switch --orphan "${BRANCH}"

source "${GITLAB_ENV}"

# Add github as a remote
git remote add github "https://anyuser:${STS_TOKEN}@github.com/DataDog/commit-headless.git"

# Create and push an initial commit, which will create the branch on the GitHub side
git commit --allow-empty -m"initial commit"

git push --set-upstream github "${BRANCH}"

# Create a commit
echo "pipeline: ${CI_PIPELINE_ID}" >> pipeline.txt
git add pipeline.txt
git commit -m"commit from commit-headless ${CI_COMMIT_SHORT_SHA}"

# Get the revision for this commit
REV=$(git rev-parse HEAD)

# And use commit-headless-dev to push it to the remote branch
GH_TOKEN="${STS_TOKEN}" /tmp/commit-headless-dev push --target DataDog/commit-headless --branch "${BRANCH}" "${REV}"

# TODO: Run the following code in an exit trap if possible to ensure things are cleaned up.

# Delete the branch on the GitHub side
git push github --delete "${BRANCH}"

# And switch back to our original branch
git switch -

# Finally, revert the config changes
if [ -n "${ORIGINAL_NAME}" ]; then
    git config user.name "${ORIGINAL_NAME}"
fi
if [ -n "${ORIGINAL_EMAIL}" ]; then
git config user.email "${ORIGINAL_EMAIL}"
fi
