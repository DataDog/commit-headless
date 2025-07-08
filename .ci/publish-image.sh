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

# TODO: Let this run without depending on the release pipeline so it can be scheduled. It can use
# either the version produced by release:publish *or* a version specified via gitlab variable.
# Instead of using prebuilt binaries in dist/ we can fetch them from the specified image.
VERSION=$(cat dist/VERSION.txt)
TARGET="registry.ddbuild.io/commit-headless-ci-image:v${VERSION}"

PLATFORMS=(
    "linux/amd64"
    "linux/arm64"
)

echo "Creating new base image ${TARGET} from ${BASE_IMAGE}"

DIGESTS=()

# For each platform, we can use `crane append` to add the architecture specific commit-headless
# binary to the dd-octo-sts-ci-base image, moving it over to our CI image tag.
# Once we've appended for each platform, we'll turn it into an index.
for PLATFORM in "${PLATFORMS[@]}"; do
    CLEANPLAT="$(echo ${PLATFORM} | tr '/' '-')"
    BIN="commit-headless-${CLEANPLAT}"
    LAYER="layer-${CLEANPLAT}.tar"

    echo "...building for ${PLATFORM}"

    $TAR -c --transform "s|^${BIN}|/usr/local/bin/commit-headless|g" -f "${LAYER}" -C ./dist "${BIN}"

    DIGESTS+=($(crane append \
        --platform="${PLATFORM}" \
        --base "${BASE_IMAGE}" \
        -f "${LAYER}" \
        -t "${TARGET}"))
    rm "${LAYER}"
done;

echo "Done creating platform specific images."

echo "..creating image index at ${TARGET}"
# Reset $TARGET to be an empty index
crane index append --tag "${TARGET}"

# And append each manifest to the index
for DIGEST in "${DIGESTS[@]}"; do
    echo "..adding platform manifest ${DIGEST}"
    crane index append "${TARGET}" --manifest "${DIGEST}"
done;

echo "Done!"
