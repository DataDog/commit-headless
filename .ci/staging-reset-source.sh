#!/bin/bash
set -euo pipefail

# Sourced from: https://github.com/DataDog/dogweb/blob/c8cd885a6736345ee941079d899a208c166b578e/tasks/gitlab/staging-reset.sh

# Used by staging reset jobs in dd-go, dogweb, profiling-backend, and web-ui.
# This script is not ideal as it sets a bunch of tokens and executes the
# legacy staging reset job, but it's the most straightforward approach to get
# rid of the Jenkins job. This script will be obsolete once we get rid of
# the staging branch.

# Add github public host key to the known_hosts
mkdir -p ~/.ssh
echo "github.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCj7ndNxQowgcQnjshcLrqPEiiphnt+VTTvDP6mHBL9j1aNUkY4Ue1gvwnGLVlOhGeYrnZaMgRK6+PKCUXaDbC7qtbW8gIkhL7aGCsOr/C56SJMy/BCZfxd1nWzAOxSDPgVsmerOBYfNqltV9/hWCqBywINIR+5dIg6JTJ72pcEpEjcYgXkE2YEFXV1JHnsKgbLWNlhScqb2UmyRkQyytRLtL+38TGxkxCflmO+5Z8CSSNY7GidjMIZ7Q4zMjA2n1nGrlTDkzwDCsw+wqFPGQA179cnfGWOWRVruj16z6XyvxvjJwbz0wQZ75XK5tKSb7FNyeIEs4TT4jk+S4dhPeAUC5y+bDYirYgM4GC7uEnztnZyaVWQ7B381AK4Qdrwt51ZqExKbQpTUNn+EjqoTwvqNj4kqx5QUCI0ThS/YkOxJCXmPUWZbhjpCg56i+2aB6CmK2JGhn57K5mj0MNdBXA4/WnwH6XoPWJzK5Nyu2zB3nAZp+S5hpQs+p1vN1/wsjk=" > ~/.ssh/known_hosts

# Pull the SSH key and use it
eval "$(ssh-agent -s)"
aws ssm get-parameter --region us-east-1 --name "ci.${REPO}.staging_reset_ssh_key" --with-decryption --query "Parameter.Value" --out text | ssh-add -

GITHUB_STAGING_RESET_TOKEN=$(aws ssm get-parameter --region us-east-1 --name "ci.${REPO}.staging_reset_github_token" --with-decryption --query "Parameter.Value" --out text)
GITHUB_STAGING_RESET_ADMIN_TOKEN=$(aws ssm get-parameter --region us-east-1 --name "ci.${REPO}.staging_reset_github_admin_token" --with-decryption --query "Parameter.Value" --out text)
GITLAB_TOKEN=$(aws ssm get-parameter --region us-east-1 --name "ci.${REPO}.staging_reset_gitlab_token" --with-decryption --query "Parameter.Value" --out text)
SLACK_TOKEN=$(aws ssm get-parameter --region us-east-1 --name "ci.${REPO}.staging_reset_slack_token" --with-decryption --query "Parameter.Value" --out text)

# Here, we clone the repository into a temporary directory, remove the remote
# named origin (GitLab), and add GitHub as `origin` since
# `git remote rename <old> <new>` is not supported on GitLab CI.
repo_directory="$(mktemp -d)/${REPO}"
git clone ./ "${repo_directory}"
cd "${repo_directory}"
git remote remove origin
git remote add origin git@github.com:DataDog/$REPO

# Legacy staging reset flow:

git config user.email "jenkins@datadoghq.com"
git config user.name "Jenkins staging reset job"
git config push.default simple

BASE_BRANCH="prod"
if [[ $REPO == "web-ui" ]]; then
    BASE_BRANCH="preprod"
fi

git fetch origin $BASE_BRANCH
git checkout $BASE_BRANCH --force
git reset --hard origin/$BASE_BRANCH
git gc --prune=now
git remote prune origin
git branch --set-upstream-to=origin/$BASE_BRANCH $BASE_BRANCH
git pull

# Set branch numbers when it's weekly staging reset scheduled job.
# Port from
# https://github.com/DataDog/dogweb/blob/33f3470c7f3ead946aea724b788bfca1fea79914/tasks/jenkins/weekly-common-staging-reset.sh.
if [[ "${WEEKLY_STAGING_RESET:-}" == "true" ]]; then
  NEW_BRANCH_NUMBER=$(printf "%02d" $(($(date +%-V) + 1)))
  # end of year hack fixes
  if [[ $NEW_BRANCH_NUMBER == "53" ]]; then
    NEW_BRANCH_NUMBER="01"
  fi

  OLD_BRANCH_NUMBER=$(grep CURRENT_STAGING .gitlab-ci.yml | head -n 1 | awk '{print $2}' | cut -d '-' -f2)

  if [[ $OLD_BRANCH_NUMBER == $NEW_BRANCH_NUMBER ]]; then
    NEW_BRANCH_NUMBER="${NEW_BRANCH_NUMBER}a"  # edge case when someone does the wrong thing manually, pre-empt and set to "${num}a"
  elif [[ $OLD_BRANCH_NUMBER == ${NEW_BRANCH_NUMBER}* ]]; then
    # someone did something the above manual thing several times, we gotta get smarter here.
    # note the space between : and -1 is *critical* #bashisms
    last_char=$(echo "${OLD_BRANCH_NUMBER: -1}" | tr "a-yA-Y" "b-zB-Z")
    NEW_BRANCH_NUMBER="${NEW_BRANCH_NUMBER}${last_char}"  # edge case when someone does the wrong thing manually, pre-empt and set to "${num}[a --> b logic]"
  fi
fi

# Fail early in case of bad parameters
[[ -z "$OLD_BRANCH_NUMBER" || -z "$NEW_BRANCH_NUMBER" || -z "$REPO" ]] && {
    echo "Missing parameters"
    exit 1
}

[[ $OLD_BRANCH_NUMBER == staging-* || $NEW_BRANCH_NUMBER == staging-* ]] && {
    echo "You should not include 'staging-' in parameters"
    exit 1
}

[[ $OLD_BRANCH_NUMBER == $NEW_BRANCH_NUMBER ]] && {
    echo "Old branch number $OLD_BRANCH_NUMBER matches new branch number $NEW_BRANCH_NUMBER. To reset the staging branch early for the week, append or bump the letter. Example: staging-20 -> staging-20a or staging-20a -> staging-20b"
    exit 1
}

if git rev-parse staging-${NEW_BRANCH_NUMBER} 2>/dev/null; then
    echo "Delete local staging branch..."
    git branch -D staging-$NEW_BRANCH_NUMBER
fi

echo "Deleting old remote staging branch..."
git push origin --delete staging-$NEW_BRANCH_NUMBER || true
echo "Creating new staging branch..."
git checkout -b staging-$NEW_BRANCH_NUMBER
git push -u origin staging-$NEW_BRANCH_NUMBER -f

sed -i 's/CURRENT_STAGING: staging-.*/CURRENT_STAGING: staging-'$NEW_BRANCH_NUMBER'/g' .gitlab-ci.yml
if [[ $(git status --porcelain) ]]; then
    echo "Changing staging branch in .gitlab-ci.yml..."
    git commit -n -m "Change staging branch in .gitlab-ci.yml to staging-$NEW_BRANCH_NUMBER" .gitlab-ci.yml

    git push origin HEAD:refs/heads/staging-reset/staging-$NEW_BRANCH_NUMBER
    PR_NUMBER=$(curl -H "Authorization: token $GITHUB_STAGING_RESET_TOKEN" -X POST https://api.github.com/repos/datadog/${REPO}/pulls -d '{"head":"staging-reset/staging-'"${NEW_BRANCH_NUMBER}"'","base":"'"${BASE_BRANCH}"'","title":"staging reset '"${NEW_BRANCH_NUMBER}"'"}' | jq -r .number)
    echo "Created pull request: https://github.com/datadog/${REPO}/pull/${PR_NUMBER}, approving and waiting until I can merge this to continue..."
    curl -H "Authorization: token $GITHUB_STAGING_RESET_ADMIN_TOKEN" -X POST https://api.github.com/repos/datadog/${REPO}/pulls/${PR_NUMBER}/reviews -d '{"event": "APPROVE"}'
    MERGED=$(curl -H "Authorization: token $GITHUB_STAGING_RESET_ADMIN_TOKEN" -X PUT https://api.github.com/repos/datadog/${REPO}/pulls/${PR_NUMBER}/merge | jq .merged)
    until [ "$MERGED" == "true" ]; do
    	echo "still not mergeable"
    	sleep 30
	MERGED=$(curl -H "Authorization: token $GITHUB_STAGING_RESET_ADMIN_TOKEN" -X PUT https://api.github.com/repos/datadog/${REPO}/pulls/${PR_NUMBER}/merge | jq .merged)
    done
else
    echo "Staging branch already up to date in .gitlab-ci.yml. Skipping."
fi

# We could get $OLD_BRANCH_NUMBER from prod's .gitlab-ci.yml instead of user
# input, but that would make this job non-idempotent (e.g. if a previous run of
# the job failed after .gitlab-ci.yml was updated but before reaching the end,
# retrying it would lead to a different outcome). So having it as a user input
# is preferrable.
git checkout staging-$OLD_BRANCH_NUMBER
if [[ $REPO == "web-ui" ]]; then
    git reset --hard origin/staging-$OLD_BRANCH_NUMBER
    git pull
else
    git fetch origin staging-$OLD_BRANCH_NUMBER
    git reset --hard origin/staging-$OLD_BRANCH_NUMBER
fi

if [ -f .gitlab-ci.yml ]; then
    echo "Disabling CI on the old branch..."
    git rm .gitlab-ci.yml
    git commit .gitlab-ci.yml -m "Remove .gitlab-ci.yml on old branch so pushes are noop"
    git push --set-upstream origin staging-$OLD_BRANCH_NUMBER
else
    echo "CI already disabled on the old branch. Skipping."
fi

current_year=$(date +%Y)
tag="archive/staging/$current_year-$OLD_BRANCH_NUMBER"
# if tag does not exist already
if ! git rev-parse ${tag} 2>/dev/null; then
    echo "Tagging old branch for archiving..."
    git tag $tag origin/staging-$OLD_BRANCH_NUMBER
    git push origin $tag
else
    echo "Old branch already tagged for archiving. Skipping."
fi
# Don't delete the old branch or someone may re-push it containing .gitlab-ci.yml

git checkout $BASE_BRANCH
git pull

# Get Gitlab project ID
slack_additional_webhook=""
if [[ $REPO == "dogweb" ]]; then
    project_id=5
elif [[ $REPO == "profiling-backend" ]]; then
    project_id=924
elif [[ $REPO == "web-ui" ]]; then
    project_id=881
    slack_additional_webhook=${SLACK_WEBHOOK_FRONTEND:-}
elif [[ $REPO == "staging-reset-testing" ]]; then
    project_id=905
else
    echo "Unknown project_id for ${REPO}"
    exit 1
fi

# Cancel useless Gitlab jobs for the old branch
echo "Stopping Gitlab jobs for the staging-$OLD_BRANCH_NUMBER branch on $REPO..."
# NB: the following request is paginated. Adjust 'per_page' parameter if needed
curl -sSf -H "PRIVATE-TOKEN: $GITLAB_TOKEN" "https://gitlab.ddbuild.io/api/v4/projects/$project_id/pipelines?per_page=100" |
    jq ".[] | select(.ref==\"staging-$OLD_BRANCH_NUMBER\") | select((.status==\"pending\") or (.status==\"running\")) | .id" |
    xargs -I {pipeline_id} bash -c "curl -sSf -X POST -H 'PRIVATE-TOKEN: $GITLAB_TOKEN' 'https://gitlab.ddbuild.io/api/v4/projects/$project_id/pipelines/{pipeline_id}/cancel'"

### Notify of new branch name on Slack
if [[ $REPO == "staging-reset-testing" ]]; then
    SLACK_WEBHOOK_URL=${SLACK_WEBHOOK_STAGING_RESET_TESTING:-}
else
    SLACK_WEBHOOK_URL=${SLACK_WEBHOOK_STAGING_HEADSUP:-}
fi

if [[ $REPO == "dogweb" ]]; then
    SLACK_MESSAGE="Staging reset of \`$REPO\` is almost done, except for migrations that have to be rolled back manually (see <https://github.com/DataDog/devops/wiki/Reset-Staging#dogweb-rolling-back-migrations|instructions>). The new branch is \`staging-$NEW_BRANCH_NUMBER\`."
else
    SLACK_MESSAGE="Staging reset of \`$REPO\` is done! The new branch is \`staging-$NEW_BRANCH_NUMBER\`."
fi

# TODO: Uncomment when we have the proper user for this
# Currently SLACK_WEBHOOK_URL doesn't have the proper authorizations.
# curl -sSf -X POST -H 'Content-type: application/json' -d "{\"text\":\"$SLACK_MESSAGE\"}" $SLACK_WEBHOOK_URL
# if [[ ! -z "$slack_additional_webhook" ]]; then
#     curl -sSf -X POST -H 'Content-type: application/json' -d "{\"text\":\"$SLACK_MESSAGE\"}" $slack_additional_webhook
# fi

### Change Slack topic
function current_staging() {
    BRANCH="prod"
    if [[ $1 == "web-ui" ]]; then
        BRANCH="preprod"
    fi
    echo $(curl -sSf -H "Authorization: token $GITHUB_STAGING_RESET_TOKEN" "https://api.github.com/repos/DataDog/$1/contents/.gitlab-ci.yml?ref=$BRANCH" |
        jq -r .content | base64 -d | grep -oP "CURRENT_STAGING: staging-\K([0-9]+[a-z]*)")
}

# channel #staging-headsup
channel="C36FYM6EB"
if [[ $REPO == "staging-reset-testing" ]]; then
    # channel #staging-reset-testing
    channel="CG8K9KL76"
fi

TOPIC_MESSAGE="dogweb: \`$(current_staging dogweb)\` | logs-backend: \`$(current_staging logs-backend)\` | profiling-backend: \`$(current_staging profiling-backend)\` | web-ui: \`$(current_staging web-ui)\` | <https://datadoghq.atlassian.net/wiki/spaces/DEVX/pages/2922972488|prod-into-staging merge conflict>"
curl -sSf -X POST -H "Authorization: Bearer $SLACK_TOKEN" -H 'Content-type: application/json; charset=utf-8' -d "{\"channel\":\"$channel\", \"topic\":\"$TOPIC_MESSAGE\"}" https://slack.com/api/conversations.setTopic
