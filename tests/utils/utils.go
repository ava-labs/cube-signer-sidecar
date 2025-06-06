package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/ava-labs/cubist-signer/config"
	"github.com/ethereum/go-ethereum/log"
	. "github.com/onsi/gomega"
)

const (
	DefaultSignerConfigFileName = "signer-config.json"
	DefaultRoleID               = "e2e_signer"
	DefaultTokenPath            = "e2e_session.json"
	DefaultKeyID                = "Key#BlsAvaIcm_0x856218c1a1a84cd4e25321fe7bde03260d2686dad5c9ddd05e77509cc0ef3114d7290810843748a2bd8bb3a2ff8c4d6e"
	DefaultSignerEndpoint       = "https://gamma.signer.cubist.dev"
	DefaultPort                 = 50051
)

func BuildCubistSigner() {
	cmd := exec.Command("./scripts/build.sh")
	log.Info("Building cubist signer")
	out, err := cmd.CombinedOutput()
	log.Info(string(out))
	Expect(err).Should(BeNil())
}

func WriteConfig(cfg *config.Config, fname string) string {
	data, err := json.MarshalIndent(cfg, "", "\t")
	Expect(err).Should(BeNil())

	f, err := os.CreateTemp(os.TempDir(), fname)
	defer f.Close()
	Expect(err).Should(BeNil())

	_, err = f.Write(data)
	Expect(err).Should(BeNil())

	configPath := f.Name()
	return configPath
}

func CreateDefaultConfig() *config.Config {
	cfg := config.Config{
		TokenFilePath:  DefaultTokenPath,
		KeyID:          DefaultKeyID,
		SignerEndpoint: DefaultSignerEndpoint,
		Port:           DefaultPort,
	}

	return &cfg
}

func RunSigner(ctx context.Context, cfgPath string) context.CancelFunc {
	cmdCtx, cancelFn := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, "./build/cubist-signer", "--config-file", cfgPath)
	fmt.Println("Running cubist signer, cmd:", cmd.String())
	err := cmd.Start()
	Expect(err).Should(BeNil())

	return func() {
		cancelFn()
		<-cmdCtx.Done()
		cmd.Wait()
	}
}
