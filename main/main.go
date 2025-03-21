package main

import (
	"context"
	"log"
	"net"
	"os"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/cubist-signer/api"
	"github.com/ava-labs/cubist-signer/signerserver"
	"google.golang.org/grpc"
)

func main() {
	if err := runServer(getEnvArgs()); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

func getEnvArgs() (string, string, string) {
	tokenFilePath := os.Getenv("TOKEN_FILE_PATH")
	if tokenFilePath == "" {
		log.Fatal("TOKEN_FILE_PATH environment variable is not set")
	}

	_, err := os.Stat(tokenFilePath)
	if err != nil {
		log.Fatalf("failed to open token file: %w", err)
	}

	keyId := os.Getenv("KEY_ID")
	if keyId == "" {
		log.Fatal("KEY_ID environment variable is not set")
	}

	endpoint := os.Getenv("SIGNER_ENDPOINT")
	if endpoint == "" {
		log.Fatal("SIGNER_ENDPOINT environment variable is not set")
	}

	return tokenFilePath, keyId, endpoint
}

func runServer(tokenFilePath string, keyID string, endpoint string) error {
	client, err := api.NewClientWithResponses(endpoint)
	if err != nil {
		log.Println("failed to create API client")
		return err
	}

	signerServer, err := signerserver.New(keyID, tokenFilePath, client)
	if err != nil {
		log.Println("failed to create signer server")
		return err
	}

	err = signerServer.RefreshToken()

	if err != nil {
		log.Println("failed to refresh token")
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signerServer.StartBackgroundTokenRefresh(ctx)

	grpcServer := grpc.NewServer()
	signer.RegisterSignerServer(grpcServer, signerServer)

	// TODO: make configurable
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Println("failed to start gRPC server")
		return err
	}

	log.Println("Starting gRPC server on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Println("failed to serve")
		return err
	}

	return nil
}
