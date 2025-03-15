package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/cubist-signer/api"
	"github.com/ava-labs/cubist-signer/signerserver"
	"google.golang.org/grpc"
)

func main() {
	tokenFilePath := os.Getenv("TOKEN_FILE_PATH")
	if tokenFilePath == "" {
		log.Fatal("TOKEN_FILE_PATH environment variable is not set")
	}

	file, err := os.Open(tokenFilePath)
	if err != nil {
		log.Fatalf("failed to open token file: %w", err)
	}
	defer file.Close()

	var tokenData signerserver.TokenData
	if err := json.NewDecoder(file).Decode(&tokenData); err != nil {
		log.Fatalf("failed to decode token file: %w", err)
	}

	orgId := tokenData.OrgID
	keyId := os.Getenv("KEY_ID")
	if keyId == "" {
		log.Fatal("KEY_ID environment variable is not set")
	}

	endpoint := os.Getenv("SIGNER_ENDPOINT")
	if endpoint == "" {
		log.Fatal("SIGNER_ENDPOINT environment variable is not set")
	}

	client, err := api.NewClientWithResponses(endpoint)
	if err != nil {
		log.Fatalf("failed to create API client: %w", err)
	}

	authData := &api.AuthData{
		EpochNum:   tokenData.SessionInfo.Epoch,
		EpochToken: tokenData.SessionInfo.EpochToken,
		OtherToken: tokenData.SessionInfo.RefreshToken,
	}

	res, err := client.SignerSessionRefreshWithResponse(context.Background(), orgId, *authData, func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", tokenData.Token)
		return nil
	})

	if err != nil {
		log.Fatalf("failed to refresh session: %w", err)
	}

	if res.JSON200 == nil {
		log.Fatalf("unexpected status code: %d", res.StatusCode())
	}

	signerServer := &signerserver.SignerServer{
		OrgId:   orgId,
		KeyId:   keyId,
		Client:  client,
		Session: res.JSON200,
	}

	go func() {
		for {
			expiryTime := time.Unix(int64(signerServer.Session.SessionInfo.AuthTokenExp), 0)
			waitDuration := time.Until(expiryTime) - time.Second

			if waitDuration < 0 {
				refreshExpiryTime := time.Unix(int64(signerServer.Session.SessionInfo.RefreshTokenExp), 0)
				if time.Until(refreshExpiryTime) < 0 {
					log.Fatalf("Refresh token expired at %v", refreshExpiryTime)
				}
				waitDuration = 0
			}

			time.Sleep(waitDuration)

			if err := signerServer.RefreshToken(); err != nil {
				log.Printf("Failed to refresh token: %w", err)
				continue
			}
		}
	}()

	grpcServer := grpc.NewServer()
	signer.RegisterSignerServer(grpcServer, signerServer)

	// TODO: make configurable
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %w", err)
	}

	log.Println("Starting gRPC server on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %w", err)
	}
}
