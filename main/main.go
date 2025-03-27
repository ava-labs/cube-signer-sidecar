package main

import (
	"context"
	"log"
	"net"
	"strconv"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/cubist-signer/api"
	"github.com/ava-labs/cubist-signer/config"
	"github.com/ava-labs/cubist-signer/signerserver"
	"google.golang.org/grpc"
)

func main() {
	if err := runServer(config.GetEnvArgs()); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

func runServer(tokenFilePath string, keyID string, endpoint string, listenerPort uint16) error {
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

	port := strconv.Itoa(int(listenerPort))

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", ":"+port)
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
