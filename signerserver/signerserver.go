package main

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -generate client -package api  -o ../api/client.go ../spec/filtered-openapi.json
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -generate types -package api  -o ../api/types.go ../spec/filtered-openapi.json

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/cubist-signer/api"
	"google.golang.org/grpc"
)

var popDst = base64.StdEncoding.EncodeToString(bls.CiphersuiteProofOfPossession.Bytes())

type SignerServer struct {
	signer.UnimplementedSignerServer
	orgId     string
	keyId     string
	client    *api.ClientWithResponses
	session   *api.NewSessionResponse
	publicKey []byte
}

func (s *SignerServer) AddAuthHeaderFn() api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", s.session.Token)
		return nil
	}
}

func (s *SignerServer) RefreshToken() error {
	authData := &api.AuthData{
		EpochNum:   s.session.SessionInfo.Epoch,
		EpochToken: s.session.SessionInfo.EpochToken,
		OtherToken: s.session.SessionInfo.RefreshToken,
	}

	res, err := s.client.SignerSessionRefreshWithResponse(context.Background(), s.orgId, *authData, s.AddAuthHeaderFn())
	if err != nil {
		return err
	}

	if res.JSON200 == nil {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	s.session = res.JSON200
	return nil
}

func (s *SignerServer) PublicKey(ctx context.Context, in *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	if s.publicKey != nil {
		publicKeyRes := &signer.PublicKeyResponse{
			PublicKey: s.publicKey,
		}

		return publicKeyRes, nil
	}

	rsp, err := s.client.GetKeyInOrg(ctx, s.orgId, s.keyId, s.AddAuthHeaderFn())
	if err != nil {
		log.Println("Error getting key in org:", err)
		return nil, err
	}

	res, err := parseGetKeyInOrgResponse(rsp)
	if err != nil {
		log.Println("Error parsing GetKeyInOrg response:", err)
		return nil, err
	}

	if res.JSONDefault != nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	log.Printf("PublicKey: %+v", res.JSON200.PublicKey)

	publicKey, err := hex.DecodeString(res.JSON200.PublicKey[2:])
	if err != nil {
		return nil, err
	}

	s.publicKey = publicKey

	publicKeyRes := &signer.PublicKeyResponse{
		PublicKey: publicKey,
	}

	return publicKeyRes, nil
}

type KeyInfo struct {
	PublicKey string `json:"public_key"`
}

type GetKeyInOrgResponse struct {
	api.GetKeyInOrgResponse
	JSON200 *KeyInfo
}

// modified version of `api.ParseGetKeyInOrgResponse`
// this code can be removed if cubist fixes with openapi-spec
func parseGetKeyInOrgResponse(rsp *http.Response) (*GetKeyInOrgResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, err
	}

	inner := api.GetKeyInOrgResponse{
		Body:         bodyBytes,
		HTTPResponse: rsp,
	}

	response := &GetKeyInOrgResponse{
		GetKeyInOrgResponse: inner,
	}

	switch {
	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && rsp.StatusCode == 200:
		var dest KeyInfo
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && true:
		var dest api.ErrorResponse
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, err
		}
		response.JSONDefault = &dest

	}

	return response, nil
}

func (s *SignerServer) sign(ctx context.Context, bytes []byte, blsDst *string) ([]byte, error) {
	msg := base64.StdEncoding.EncodeToString(bytes)
	blobSignReq := &api.BlobSignRequest{
		MessageBase64: msg,
		BlsDst:        blsDst,
	}

	res, err := s.client.BlobSignWithResponse(ctx, s.orgId, s.keyId, *blobSignReq, s.AddAuthHeaderFn())
	if err != nil {
		return nil, err
	}

	if res.JSON200 == nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	signature, err := hex.DecodeString(res.JSON200.Signature[2:])
	if err != nil {
		return nil, err
	}

	return signature, nil
}

func (s *SignerServer) Sign(ctx context.Context, in *signer.SignRequest) (*signer.SignResponse, error) {
	signature, err := s.sign(ctx, in.Message, nil)
	if err != nil {
		return nil, err
	}

	signatureRes := &signer.SignResponse{
		Signature: signature,
	}

	return signatureRes, nil
}

func (s *SignerServer) ProofOfPossession(ctx context.Context, in *signer.SignProofOfPossessionRequest) (*signer.SignProofOfPossessionResponse, error) {
	signature, err := s.sign(ctx, in.Message, &popDst)
	if err != nil {
		return nil, err
	}

	signatureRes := &signer.SignProofOfPossessionResponse{
		Signature: signature,
	}

	return signatureRes, nil
}

type TokenData struct {
	api.NewSessionResponse
	OrgID  string `json:"org_id"`
	RoleID string `json:"role_id"`
}

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

	var tokenData TokenData
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

	signerServer := &SignerServer{
		orgId:   orgId,
		keyId:   keyId,
		client:  client,
		session: res.JSON200,
	}

	go func() {
		for {
			expiryTime := time.Unix(int64(signerServer.session.SessionInfo.AuthTokenExp), 0)
			waitDuration := time.Until(expiryTime) - time.Second

			if waitDuration < 0 {
				refreshExpiryTime := time.Unix(int64(signerServer.session.SessionInfo.RefreshTokenExp), 0)
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
