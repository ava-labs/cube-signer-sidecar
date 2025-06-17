#!/usr/bin/env bash

set -e

# Build the tests binary
go run github.com/onsi/ginkgo/v2/ginkgo build ./tests/

# Run the tests
echo "Running e2e tests..."
RUN_E2E=true LOG_LEVEL=${LOG_LEVEL:-"info"} ./tests/tests.test \
    --ginkgo.vv \
    --ginkgo.label-filter=${GINKGO_LABEL_FILTER:-""} \
    --ginkgo.focus=${GINKGO_FOCUS:-""} 

echo "e2e tests finished"
