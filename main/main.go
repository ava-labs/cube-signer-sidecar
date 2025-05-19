package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/cubist-signer/api"
	"github.com/ava-labs/cubist-signer/config"
	"github.com/ava-labs/cubist-signer/signerserver"
	"google.golang.org/grpc"
)

func main() {
	fs := config.BuildFlagSet()
	if err := fs.Parse(os.Args[1:]); err != nil {
		panic(fmt.Errorf("couldn't parse flags: %w", err))
	}

	// If the help flag is set, output the usage text then exit
	help, err := fs.GetBool(config.HelpKey)
	if err != nil {
		panic(fmt.Errorf("error reading %s flag value: %w", config.HelpKey, err))
	}

	if help {
		fs.Usage()
		os.Exit(0)
	}

	v, err := config.BuildViper(fs)
	if err != nil {
		panic(fmt.Errorf("couldn't configure flags: %w", err))
	}

	cfg, err := config.NewConfig(v)
	if err != nil {
		panic(fmt.Errorf("couldn't build config: %w", err))
	}

	if err := runServer(cfg); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}

func runServer(cfg config.Config) error {
	client, err := api.NewClientWithResponses(cfg.SignerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	signerServer, err := signerserver.New(cfg.KeyID, cfg.TokenFilePath, client)
	if err != nil {
		return fmt.Errorf("failed to create signer server: %w", err)
	}

	err = signerServer.RefreshToken()
	if err != nil {
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signerServer.StartBackgroundTokenRefresh(ctx)

	grpcServer := grpc.NewServer()
	signer.RegisterSignerServer(grpcServer, signerServer)

	port := strconv.Itoa(int(cfg.Port))

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	log.Printf("Starting gRPC server on port %s...", port)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}
