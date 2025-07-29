package utils

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/ava-labs/cube-signer-sidecar/config"
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
	log.Println("Building cube-signer-sidecar")
	out, err := cmd.CombinedOutput()
	log.Println(string(out))
	Expect(err).Should(BeNil())
}

func WriteConfig(cfg *config.Config, fname string) string {
	data, err := json.MarshalIndent(cfg, "", "\t")
	Expect(err).Should(BeNil())

	f, err := os.CreateTemp(os.TempDir(), fname)
	Expect(err).Should(BeNil())
	Expect(f).ShouldNot(BeNil())
	defer f.Close()

	_, err = f.Write(data)
	Expect(err).Should(BeNil())

	configPath := f.Name()
	return configPath
}

func CreateDefaultConfig() *config.Config {
	return &config.Config{
		TokenFilePath:  DefaultTokenPath,
		SignerEndpoint: DefaultSignerEndpoint,
		Port:           DefaultPort,
		UserToken:      "",
		OrgId:          "",
		KeyId:          DefaultKeyID,
		RoleId:         DefaultRoleID,
	}
}

func RunSigner(ctx context.Context, cfgPath string) context.CancelFunc {
	cmdCtx, cancelFn := context.WithCancel(ctx)
	cmd := exec.CommandContext(cmdCtx, "./build/cube-signer-sidecar", "--config-file", cfgPath)

	// Set up a pipe to capture the command's output
	cmdStdOutReader, err := cmd.StdoutPipe()
	Expect(err).Should(BeNil())
	cmdStdErrReader, err := cmd.StderrPipe()
	Expect(err).Should(BeNil())

	log.Println("Running cube-signer-sidecar, cmd:", cmd.String())
	err = cmd.Start()
	Expect(err).Should(BeNil())

	go func() {
		scanner := bufio.NewScanner(cmdStdOutReader)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	go func() {
		scanner := bufio.NewScanner(cmdStdErrReader)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()

	return func() {
		cancelFn()
		cmd.Wait()
	}
}
