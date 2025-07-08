#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

TAR=$(which tar)
# bsdtar is default on macOS, so let's use gtar if it exists
# this is non-exhaustive, but workable for me since i have gtar (brew install coreutils)
if command -v gtar >/dev/null 2>&1; then
    TAR=$(which gtar)
fi;

BASE_IMAGE_REPO="registry.ddbuild.io/images/dd-octo-sts-ci-base"
BASE_TAG="2025.06-1"
BASE_IMAGE="${BASE_IMAGE_REPO}:${BASE_TAG}"

VERSION=$(cat dist/VERSION.txt)
TARGET="registry.ddbuild.io/commit-headless-ci-image:v${VERSION}"

PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
)

echo "Creating new base image ${TARGET} from ${BASE_IMAGE}"

# For each platform, we need to create a tarball that contains /usr/local/bin/commit-headless
# and append it via crane append to the base image above.
for PLATFORM in "${PLATFORMS[@]}"; do
    BIN="commit-headless-$(echo "${PLATFORM}" | tr '/' '-')"
    echo "...building for ${PLATFORM}"

    $TAR -c --transform "s|^${BIN}|/usr/local/bin/commit-headless|g" -f layer.tar -C ./dist "${BIN}"

    crane append \
        --platform="${PLATFORM}" \
        --base "${BASE_IMAGE}" \
        -f layer.tar \
        -t "${TARGET}"
    rm layer.tar

done;

echo "Done!"
