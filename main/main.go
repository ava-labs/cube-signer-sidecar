package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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

// Bound the time spent on any single CubeSigner API call, so that a hung
// upstream can't stall a signing request or the token refresh loop forever.
const apiRequestTimeout = 30 * time.Second

func runServer(cfg config.Config) error {
	httpClient := &http.Client{Timeout: apiRequestTimeout}

	client, err := api.NewClientWithResponses(cfg.SignerEndpoint, api.WithHTTPClient(httpClient))
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	signerServer, err := signerserver.New(cfg.KeyID, cfg.TokenFilePath, client)
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
	address := net.JoinHostPort(cfg.BindAddress, port)

	// The server authenticates no one and signs arbitrary bytes with the
	// validator's BLS key, so anything that can reach it can forge signatures.
	if !cfg.IsLoopbackBindAddress() {
		log.Printf(
			"WARNING: binding to %s exposes an unauthenticated signing oracle for key %s beyond this host; "+
				"ensure the port is restricted to the AvalancheGo node by other means",
			cfg.BindAddress, cfg.KeyID,
		)
	}

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("failed to start gRPC server: %w", err)
	}

	api.HandleHealthCheck()

	// Stop serving once a shutdown signal cancels the context, letting in-flight
	// signing requests finish first.
	go func() {
		<-ctx.Done()
		log.Println("Shutting down gRPC server...")
		grpcServer.GracefulStop()
	}()

	log.Printf("Starting gRPC server on %s...", address)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %w", err)
	}

	return nil
}

func handleSystemSignals(cancel context.CancelFunc) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	sig := <-sigChan
	log.Printf("Received os signal: %s", sig.String())

	// Cancel the parent context
	cancel()
}
