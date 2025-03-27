package config

import (
	"log"
	"os"
)

func GetEnvArgs() (string, string, string, uint16) {
	tokenFilePath := os.Getenv("TOKEN_FILE_PATH")
	if tokenFilePath == "" {
		log.Fatal("TOKEN_FILE_PATH environment variable is not set")
	}

	_, err := os.Stat(tokenFilePath)
	if err != nil {
		log.Fatalf("failed to open token file: %v", err)
	}

	keyID := os.Getenv("KEY_ID")
	if keyID == "" {
		log.Fatal("KEY_ID environment variable is not set")
	}

	endpoint := os.Getenv("SIGNER_ENDPOINT")
	if endpoint == "" {
		log.Fatal("SIGNER_ENDPOINT environment variable is not set")
	}

	// TODO: make the port configurable
	return tokenFilePath, keyID, endpoint, 50051
}
