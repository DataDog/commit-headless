#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Print commands, but do not expand variables (avoid leaking secrets)
set -o verbose

VERSION=$(cat ./dist/VERSION.txt)

IMAGE_TAG="${IMAGE_REPO}:v${VERSION}-pre.${CI_PIPELINE_ID}-${CI_COMMIT_SHORT_SHA}"
echo "Version: ${VERSION}"
echo "Tag: ${IMAGE_TAG}"

docker buildx build --tag ${IMAGE_TAG} --label target=build --push --metadata-file image-metadata.json --file Dockerfile ./dist/

# If we're not on the default branch, don't sign
if [[ "$CI_COMMIT_BRANCH" == "$CI_DEFAULT_BRANCH" ]]; then
    ddsign sign "${IMAGE_TAG}" --docker-metadata-file image-metadata.json
fi
