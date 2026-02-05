package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/cube-signer-sidecar/api"
	"github.com/ava-labs/cube-signer-sidecar/config"
	"github.com/ava-labs/cube-signer-sidecar/signerserver"
	"google.golang.org/grpc"
)

func main() {
	fs := config.BuildFlagSet()
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatalf("couldn't parse flags: %s", err)
	}

	// If the help flag is set, output the usage text then exit
	help, err := fs.GetBool(config.HelpKey)
	if err != nil {
		log.Fatalf("error reading %s flag value: %s", config.HelpKey, err)
	}

	if help {
		fs.Usage()
		os.Exit(0)
	}

	v, err := config.BuildViper(fs)
	if err != nil {
		log.Fatalf("couldn't configure flags: %s", err)
	}

	cfg, err := config.NewConfig(v)
	if err != nil {
		log.Fatalf("couldn't build config: %s", err)
	}

	if err := runServer(cfg); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
	log.Println("server exited gracefully")
}

func runServer(cfg config.Config) error {
	client, err := api.NewClientWithResponses(cfg.SignerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	signerServer, err := signerserver.New(client, cfg)
	if err != nil {
		return fmt.Errorf("failed to create signer server: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle os signals
	go handleSystemSignals(cancel)

	signerServer.StartBackgroundTokenRefresh(ctx)

	grpcServer := grpc.NewServer()
	signer.RegisterSignerServer(grpcServer, signerServer)

	port := strconv.Itoa(int(cfg.Port))

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", ":"+port)
	if err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	api.HandleHealthCheck()

	log.Printf("Starting gRPC server on port %s...", port)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

func handleSystemSignals(cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, os.Kill)

	sig := <-sigChan
	log.Printf("Received os signal: %s", sig.String())

	// Cancel the parent context
	cancel()
}