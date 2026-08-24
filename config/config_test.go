package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTokenFile(t *testing.T, mode os.FileMode) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "token.json")
	require.NoError(t, os.WriteFile(path, []byte("{}"), mode))
	// WriteFile is subject to umask, so set the mode explicitly.
	require.NoError(t, os.Chmod(path, mode))

	return path
}

func validConfig(t *testing.T) Config {
	t.Helper()

	return Config{
		TokenFilePath:  newTokenFile(t, 0600),
		KeyID:          "Key#BlsAvaIcm_0xabc",
		SignerEndpoint: "https://gamma.signer.cubist.dev",
		Port:           defaultPort,
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := validConfig(t)
	require.NoError(t, cfg.Validate())
}

// The session token is sent to the endpoint as a bearer credential.
func TestValidateRejectsNonHTTPSEndpoint(t *testing.T) {
	for _, endpoint := range []string{
		"http://gamma.signer.cubist.dev",
		"gamma.signer.cubist.dev",
		"https://",
	} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.SignerEndpoint = endpoint
			require.Error(t, cfg.Validate())
		})
	}
}
