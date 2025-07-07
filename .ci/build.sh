#!/bin/bash
set -euo pipefail
IFS=$'\n\t'

# Print commands, but do not expand variables (avoid leaking secrets)
set -o verbose

go build -o ./dist/commit-headless -buildvcs=false .
./dist/commit-headless version | awk '{print $3}' > ./dist/VERSION.txt
