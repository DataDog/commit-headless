#!/bin/bash
# http://redsymbol.net/articles/unofficial-bash-strict-mode/
set -euo pipefail
IFS=$'\n\t'

# Get our two tokens from dd-octo-sts

# XXX: This policy does not exist yet and our example is reading the script from the current
# repository so we're reusing GITHUB_TOKEN below
# export READ_SCRIPT_TOKEN=$(dd-octo-sts token --scope=DataDog --policy=gitlab.reset-staging-dl-script)
export GITHUB_TOKEN=$(dd-octo-sts token --scope="DataDog/${REPO}" --policy=gitlab.reset-staging)
export READ_SCRIPT_TOKEN="${GITHUB_TOKEN}"

# Download the actual reset script from the upstream.
# In normal usage this'd be from dogweb but in our case we're actually downloading it from the same
# repository on the same branch.
curl \
  -H "Authorization: token ${READ_SCRIPT_TOKEN}" \
  -H "Accept: application/vnd.github.v3.raw" \
  'https://api.github.com/repos/DataDog/commit-headless/contents/.ci/staging-reset-source.sh?ref=test-weekly-reset' \
  | bash
