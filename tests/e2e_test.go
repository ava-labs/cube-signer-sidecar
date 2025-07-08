package tests

import (
	"os"
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/cubist-signer/tests/utils"
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
		"cubist-signer-e2e",
		logging.NewWrappedCore(
			logLevel,
			os.Stdout,
			logging.JSON.ConsoleEncoder(),
		),
	)

	log.Info("Building cubist signer")
	utils.BuildCubistSigner()
	log.Info("Set up ginkgo before suite")
})

var _ = ginkgo.Describe("Cubist Signer Service Integration Tests", func() {
	ginkgo.It("Basic Signing", func() {
		BasicSigning(log)
	})
})
