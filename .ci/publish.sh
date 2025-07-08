#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

VERSION=$(cat dist/VERSION.txt)
REF=$(cat image-metadata.json | jq -r '."containerimage.digest"')
SOURCE=${IMAGE_REPO}@${REF}
TARGET=${PUBLISH_IMAGE_REPO}:v${VERSION}

echo "Publishing prerelease version ${VERSION} from ref ${REF}"

echo "..making sure the image does not already exist"
# If the target already exists, fail the build
crane manifest ${TARGET} >/dev/null 2>&1 && { echo "Version ${VERSION} already exists, not continuing."; exit 1; }

# Copy to vX.Y.Z
echo "..copying to ${TARGET}"
crane copy ${SOURCE} ${TARGET}

# Tag vX.Y.Z as the latest
# TODO: Also tag vX and vX.Y
# TODO: And print full references to each
echo "..tagging as latest"
crane tag ${TARGET} latest

echo "Done!"
