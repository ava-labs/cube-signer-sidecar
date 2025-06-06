package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/rpcsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/cubist-signer/tests/utils"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func BasicSigning(log logging.Logger) {
	log.Info("Basic signing")
	cfg := utils.CreateDefaultConfig()
	configPath := utils.WriteConfig(cfg, utils.DefaultSignerConfigFileName)
	log.Info("Config written", zap.String("path", configPath))

	cancelFn := utils.RunSigner(context.Background(), configPath)
	defer cancelFn()

	clientConn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()))
	Expect(err).Should(BeNil())

	// wait for the signer to start
	time.Sleep(2 * time.Second)

	signerClient, err := rpcsigner.NewClient(context.Background(), clientConn)
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
