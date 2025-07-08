#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

VERSION=$(cat ./dist/VERSION.txt)

IMAGE_TAG="${IMAGE_REPO}:v${VERSION}-pre.${CI_PIPELINE_ID}-${CI_COMMIT_SHORT_SHA}"
echo "Version: ${VERSION}"
echo "Tag: ${IMAGE_TAG}"

echo "Building prerelease image ${IMAGE_TAG}"

docker buildx build \
    --tag=${IMAGE_TAG} \
    --label=target=build \
    --metadata-file=image-metadata.json \
    --file=Dockerfile \
    --platform="linux/amd64,linux/arm64,darwin/amd64,darwin/arm64" \
    --push \
    ./dist/

# If we're not on the default branch, don't sign
if [[ "$CI_COMMIT_BRANCH" == "$CI_DEFAULT_BRANCH" ]]; then
    echo "..signing image with ddsign"
    ddsign sign "${IMAGE_TAG}" --docker-metadata-file image-metadata.json
fi

echo "Done!"
