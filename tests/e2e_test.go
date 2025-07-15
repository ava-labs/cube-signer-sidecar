package tests

import (
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/cubist-signer-sidecar/tests/utils"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var log logging.Logger

func TestE2E(t *testing.T) {
	if os.Getenv("RUN_E2E") == "" {
		t.Skip("RUN_E2E is not set, skipping E2E tests")
	}
	RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logLevel := logging.Info
	// var ctx context.Context
	log = logging.NewLogger(
		"cubist-signer-sidecar-e2e",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	log.Info("Building cubist-signer-sidecar")
	utils.BuildCubistSigner()
	log.Info("Set up ginkgo before suite")
})

var _ = ginkgo.Describe("cubist-signer-sidecar e2e tests", func() {
	ginkgo.It("Basic Signing", func() {
		BasicSigning(log)
	})
})
