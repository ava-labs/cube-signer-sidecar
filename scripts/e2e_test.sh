#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

BASE_PATH=$(
    cd "$(dirname "${BASH_SOURCE[0]}")"
    cd .. && pwd
)

# Build the tests binary
go run github.com/onsi/ginkgo/v2/ginkgo build "${BASE_PATH}/tests/"

# Run the tests
echo "Running e2e tests..."
RUN_E2E=true LOG_LEVEL=${LOG_LEVEL:-"info"} "${BASE_PATH}/tests/tests.test" \
    --ginkgo.vv \
    --ginkgo.label-filter=${GINKGO_LABEL_FILTER:-""} \
    --ginkgo.focus=${GINKGO_FOCUS:-""} 

echo "e2e tests finished"
