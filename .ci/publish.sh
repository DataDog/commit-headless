#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Print commands, but do not expand variables (avoid leaking secrets)
set -o verbose

VERSION=$(cat dist/VERSION.txt)
REF=$(cat image-metadata.json | jq -r '."containerimage.digest"')
SOURCE=${IMAGE_REPO}@${REF}
TARGET=${PUBLISH_IMAGE_REPO}:v${VERSION}

# If the target already exists, fail the build
crane manifest ${TARGET} >/dev/null 2>&1 && { echo "Version ${VERSION} already exists, not continuing."; exit 1; }

# Copy to vX.Y.Z
crane copy ${SOURCE} ${TARGET}

# Tag vX.Y.Z as the latest
# TODO: Also tag vX and vX.Y
# TODO: And print full references to each
crane tag ${TARGET} latest
