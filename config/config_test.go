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
		BindAddress:    defaultBindAddress,
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

// The token file is a bearer credential for the signing role.
func TestValidateRejectsWorldReadableTokenFile(t *testing.T) {
	for _, mode := range []os.FileMode{0644, 0640, 0604, 0666} {
		t.Run(mode.String(), func(t *testing.T) {
			cfg := validConfig(t)
			cfg.TokenFilePath = newTokenFile(t, mode)

			err := cfg.Validate()
			require.ErrorContains(t, err, "must not be readable by group or others")
		})
	}
}

func TestValidateBindAddress(t *testing.T) {
	tests := []struct {
		bindAddress string
		wantErr     bool
	}{
		{bindAddress: "127.0.0.1"},
		{bindAddress: "::1"},
		{bindAddress: "0.0.0.0"}, // permitted, but warned about at startup
		{bindAddress: "", wantErr: true},
		{bindAddress: "localhost", wantErr: true},
		{bindAddress: "127.0.0.1:50051", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.bindAddress, func(t *testing.T) {
			cfg := validConfig(t)
			cfg.BindAddress = test.bindAddress

			if test.wantErr {
				require.Error(t, cfg.Validate())
				return
			}
			require.NoError(t, cfg.Validate())
		})
	}
}

func TestIsLoopbackBindAddress(t *testing.T) {
	tests := map[string]bool{
		"127.0.0.1": true,
		"127.0.0.2": true,
		"::1":       true,
		"0.0.0.0":   false,
		"10.0.0.5":  false,
		"":          false,
		"localhost": false,
	}

	for bindAddress, isLoopback := range tests {
		t.Run(bindAddress, func(t *testing.T) {
			cfg := Config{BindAddress: bindAddress}
			require.Equal(t, isLoopback, cfg.IsLoopbackBindAddress())
		})
	}
}

// The signer server must not be reachable off-host unless explicitly configured.
func TestDefaultBindAddressIsLoopback(t *testing.T) {
	cfg := Config{BindAddress: defaultBindAddress}
	require.True(t, cfg.IsLoopbackBindAddress())
}

func TestBuildConfigDefaults(t *testing.T) {
	require := require.New(t)

	v, err := BuildViper(BuildFlagSet())
	require.NoError(err)

	cfg, err := BuildConfig(v)
	require.NoError(err)

	require.Equal(defaultBindAddress, cfg.BindAddress)
	require.Equal(uint16(defaultPort), cfg.Port)
}
