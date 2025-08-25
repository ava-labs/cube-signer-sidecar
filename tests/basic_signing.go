package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/rpcsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/cube-signer-sidecar/tests/utils"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

func BasicSigning(log logging.Logger) {
	cfg := utils.CreateDefaultConfig()
	configPath := utils.WriteConfig(cfg, utils.DefaultSignerConfigFileName)
	log.Info("Config written", zap.String("path", configPath))

	cancelFn := utils.RunSigner(context.Background(), configPath)
	defer cancelFn()

	// wait for the signer to start
	time.Sleep(2 * time.Second)

	signerClient, err := rpcsigner.NewClient(context.Background(), "127.0.0.1:50051")
	Expect(err).Should(BeNil())

	// generate random bytes
	msg := make([]byte, 32)
	_, err = rand.Read(msg)
	Expect(err).Should(BeNil())

	log.Info("Signing message", zap.String("message", fmt.Sprintf("%x", msg)))
	sig, err := signerClient.Sign(msg)
	Expect(err).Should(BeNil())

	valid := bls.Verify(signerClient.PublicKey(), sig, msg)
	Expect(valid).Should(BeTrue())
}
