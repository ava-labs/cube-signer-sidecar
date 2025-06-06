#!/usr/bin/env bash
set -o errexit
set -o nounset
set -o pipefail

BASE_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)


binary_path="build/cubist-signer"

last_git_tag=$(git describe --tags --abbrev=0 2>/dev/null) || last_git_tag="v0.0.0-dev"
echo "Building Cubist Signer Version: $last_git_tag at $binary_path"
go build -ldflags "-X 'main.version=$last_git_tag'" -o "$binary_path" "main/"*.go