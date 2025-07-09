package config

import (
	"fmt"
	"os"

	"github.com/spf13/pflag"
)

const (
	// Command line option keys and flags
	ConfigFileKey = "config-file"
	VersionKey    = "version"
	HelpKey       = "help"

	TokenFilePathKey = "token-file-path"
	EndpointKey      = "signer-endpoint"
	PortKey          = "port"
)

func BuildFlagSet() *pflag.FlagSet {
	fs := pflag.NewFlagSet("cubist-signer", pflag.ExitOnError)
	fs.Bool(HelpKey, false, "Display this help message and exit")
	fs.String(ConfigFileKey, "", "Path to the config file")

	fs.String(TokenFilePathKey, "", "Path to the token file")
	fs.String(EndpointKey, "", "Signer endpoint")
	fs.Uint16(PortKey, defaultPort, "Port to listen on")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags]\n\n", os.Args[0])
		fs.PrintDefaults()
	}
	return fs
}
