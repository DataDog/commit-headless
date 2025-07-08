#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

PLATFORMS=(
    "darwin-amd64"
    "darwin-arm64"
    "linux-amd64"
    "linux-arm64"
)

# Create the 'dist' directory if it doesn't exist
mkdir -p dist

for PLATFORM in "${PLATFORMS[@]}"; do
    # Extract GOOS and GOARCH from the platform string
    GOOS=${PLATFORM%%-*}
    GOARCH=${PLATFORM##*-}

    echo "Building for $GOOS/$GOARCH..."

    # Build the executable
    env GOOS=$GOOS GOARCH=$GOARCH go build -o "dist/commit-headless-$GOOS-$GOARCH" -buildvcs=false .
done

echo "Build completed."

# Figure out which one to execute based on system plat/arch
OS_NAME=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
esac

"./dist/commit-headless-$OS_NAME-$ARCH" version | awk '{print $3}' > ./dist/VERSION.txt
