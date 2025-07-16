#!/bin/bash
# http://redsymbol.net/articles/unofficial-bash-strict-mode/
set -euo pipefail
IFS=$'\n\t'

GITHUB_STAGING_RESET_TOKEN=$(aws ssm get-parameter --region us-east-1 --name "ci.${REPO}.staging_reset_github_token" --with-decryption --query "Parameter.Value" --out text)

 # See https://github.com/DataDog/dogweb/blob/prod/tasks/gitlab/staging-reset.sh
curl \
  -H "Authorization: token ${GITHUB_STAGING_RESET_TOKEN}" \
  -H "Accept: application/vnd.github.v3.raw" \
  https://api.github.com/repos/DataDog/dogweb/contents/tasks/gitlab/staging-reset.sh \
  | bash
