package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	defaultPort = 50051
)

type Config struct {
	TokenFilePath  string `mapstructure:"token-file-path" json:"token-file-path"`
	KeyID          string `mapstructure:"key-id" json:"key-id"`
	SignerEndpoint string `mapstructure:"signer-endpoint" json:"signer-endpoint"`
	Port           uint16 `mapstructure:"port" json:"port"`
}

func (cfg *Config) Validate() error {
	if cfg.TokenFilePath == "" {
		return fmt.Errorf("token-file-path is required")
	}

	// Just check for existence of the file here
	// Any other potential errors will be caught at time of usage
	if _, err := os.Stat(cfg.TokenFilePath); os.IsNotExist(err) {
		return fmt.Errorf("token-file-path does not exist: %s", cfg.TokenFilePath)
	}

	if cfg.KeyID == "" {
		return fmt.Errorf("key-id is required")
	}

	if cfg.SignerEndpoint == "" {
		return fmt.Errorf("signer-endpoint is required")
	}
	return nil
}

func NewConfig(v *viper.Viper) (Config, error) {
	cfg, err := BuildConfig(v)
	if err != nil {
		return cfg, err
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// Build the viper instance. The config file must be provided via the command line flag or environment variable.
// All config keys may be provided via config file or environment variable.
func BuildViper(fs *pflag.FlagSet) (*viper.Viper, error) {
	v := viper.New()
	v.AutomaticEnv()
	// Map flag names to env var names. Flags are capitalized, and hyphens are replaced with underscores.
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	if err := v.BindPFlags(fs); err != nil {
		return nil, err
	}

	// Load the config file if provided
	if v.IsSet(ConfigFileKey) {
		filename := v.GetString(ConfigFileKey)
		v.SetConfigFile(filename)
		v.SetConfigType("json")
		if err := v.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	return v, nil
}

// BuildConfig constructs the relayer config using Viper.
// The following precedence order is used. Each item takes precedence over the item below it:
//  1. Flags
//  2. Environment variables
//  3. Config file
//
// Returns the Config
func BuildConfig(v *viper.Viper) (Config, error) {
	// Set default values
	v.SetDefault(PortKey, defaultPort)

	// Build the config from Viper
	var cfg Config

	if err := v.Unmarshal(&cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal viper config: %w", err)
	}

	return cfg, nil
}
