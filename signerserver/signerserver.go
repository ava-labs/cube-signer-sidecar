package signerserver

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
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/cube-signer-sidecar/api"
	"github.com/ava-labs/cube-signer-sidecar/config"
)

var popDst = base64.StdEncoding.EncodeToString(bls.CiphersuiteProofOfPossession.Bytes())

var MAX_SESSION_LIFETIME int64 = 31536000 // 1 year

type SignerServer struct {
	signer.UnimplementedSignerServer
	client        *api.ClientWithResponses
	tokenData     *tokenData
	tokenFilePath string
	keyId         string
	publicKey     []byte
}

func New(client *api.ClientWithResponses, cfg config.Config) (*SignerServer, error) {
	var tokenData tokenData

	if cfg.TokenFilePath != "" {
		tokenFile, err := os.Open(cfg.TokenFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to open token file: %w", err)
		}

		if err := json.NewDecoder(tokenFile).Decode(&tokenData); err != nil {
			return nil, fmt.Errorf("failed to decode token data: %w", err)
		}
	} else {
		newSessionResponse, err := createNewSession(client, cfg.UserToken, cfg.RoleId, cfg.OrgId)
		if err != nil {
			return nil, fmt.Errorf("failed to create new session: %w", err)
		}
		tokenData.NewSessionResponse = *newSessionResponse
	}

	return &SignerServer{
		client:        client,
		tokenData:     &tokenData,
		tokenFilePath: cfg.TokenFilePath,
		keyId:         cfg.KeyId,
	}, nil
}

func (s *SignerServer) addAuthHeaderFn() api.RequestEditorFn {
	return addAuthHeaderFn(s.tokenData.Token)
}

func addAuthHeaderFn(token string) api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", token)
		return nil
	}
}

func (s *SignerServer) RefreshToken() error {
	authData := s.tokenData.toAuthData()

	res, err := s.client.SignerSessionRefreshWithResponse(context.Background(), *s.tokenData.OrgId, *authData, s.addAuthHeaderFn())
	if err != nil {
		return fmt.Errorf("failed to refresh session: %w", err)
	}
	if res.JSON200 == nil {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	s.tokenData.NewSessionResponse = *res.JSON200
	return s.saveTokenData()
}

func createNewSession(client *api.ClientWithResponses, token string, roleId string, orgId string) (*api.NewSessionResponse, error) {
	res, err := client.CreateRoleTokenWithResponse(
		context.Background(),
		orgId,
		roleId,
		api.CreateTokenRequest{Purpose: "bls signing"},
		addAuthHeaderFn(token),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh session: %w", err)
	}
	if res.JSON200 == nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	return res.JSON200, nil
}

func (s *SignerServer) saveTokenData() error {
	// Skip saving if no token file path is provided
	if s.tokenFilePath == "" {
		return nil
	}

	file, err := os.OpenFile(s.tokenFilePath, os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open token file: %w", err)
	}
	defer file.Close()

	log.Println("Saving token data")

	return json.NewEncoder(file).Encode(s.tokenData)
}

func (s *SignerServer) StartBackgroundTokenRefresh(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				expiryTime := time.Unix(int64(s.tokenData.SessionInfo.AuthTokenExp), 0)
				waitDuration := time.Until(expiryTime) - time.Second

				log.Printf("Waiting %s until refreshing token", waitDuration)

				if waitDuration < 0 {
					refreshExpiryTime := time.Unix(int64(s.tokenData.SessionInfo.RefreshTokenExp), 0)
					if time.Until(refreshExpiryTime) < 0 {
						log.Fatalf("Refresh token expired at %v", refreshExpiryTime)
					}
					waitDuration = 0
				}

				timer := time.NewTimer(waitDuration)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return
				case <-timer.C:
					if err := s.RefreshToken(); err != nil {
						log.Printf("Failed to refresh token: %v", err)
						continue
					}
				}
			}
		}
	}()
}

func (s *SignerServer) PublicKey(ctx context.Context, in *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	log.Println("Serving pubkey request")

	if s.publicKey != nil {
		log.Println("Returning cached pubkey")
		publicKeyRes := &signer.PublicKeyResponse{
			PublicKey: s.publicKey,
		}

		return publicKeyRes, nil
	}

	rsp, err := s.client.GetKeyInOrg(ctx, *s.tokenData.OrgId, s.keyId, s.addAuthHeaderFn())
	if err != nil {
		return nil, fmt.Errorf("failed to get key in org: %w", err)
	}

	res, err := parseGetKeyInOrgResponse(rsp)
	if err != nil {
		return nil, fmt.Errorf("failed to parse GetKeyInOrg response: %w", err)
	}

	if res.JSONDefault != nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	publicKey, err := hex.DecodeString(res.JSON200.PublicKey[2:])
	if err != nil {
		return nil, fmt.Errorf("failed to decode public key: %w", err)
	}

	log.Println("Public key: ", hex.EncodeToString(publicKey))

	s.publicKey = publicKey

	return &signer.PublicKeyResponse{
		PublicKey: publicKey,
	}, nil
}

type KeyInfo struct {
	PublicKey string `json:"public_key"`
}

type GetKeyInOrgResponse struct {
	api.GetKeyInOrgResponse
	JSON200 *KeyInfo
}

// modified version of `api.ParseGetKeyInOrgResponse`
// this code can be removed if Cubist fixes the openapi-spec
func parseGetKeyInOrgResponse(rsp *http.Response) (*GetKeyInOrgResponse, error) {
	bodyBytes, err := io.ReadAll(rsp.Body)
	defer func() { _ = rsp.Body.Close() }()
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
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
			return nil, fmt.Errorf("failed to unmarshal body: %w", err)
		}
		response.JSON200 = &dest

	case strings.Contains(rsp.Header.Get("Content-Type"), "json") && true:
		var dest api.ErrorResponse
		if err := json.Unmarshal(bodyBytes, &dest); err != nil {
			return nil, fmt.Errorf("failed to unmarshal body: %w", err)
		}
		response.JSONDefault = &dest

	}

	return response, nil
}

func (s *SignerServer) sign(ctx context.Context, bytes []byte, blsDst *string) ([]byte, error) {
	log.Println("Signing: ", hex.EncodeToString(bytes))

	msg := base64.StdEncoding.EncodeToString(bytes)
	blobSignReq := &api.BlobSignRequest{
		MessageBase64: msg,
		BlsDst:        blsDst,
	}

	res, err := s.client.BlobSignWithResponse(ctx, *s.tokenData.OrgId, s.keyId, *blobSignReq, s.addAuthHeaderFn())
	if err != nil {
		return nil, fmt.Errorf("failed to sign blob: %w", err)
	}

	if res.JSON200 == nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	return hex.DecodeString(res.JSON200.Signature[2:])
}

func (s *SignerServer) Sign(ctx context.Context, in *signer.SignRequest) (*signer.SignResponse, error) {
	signature, err := s.sign(ctx, in.Message, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	return &signer.SignResponse{
		Signature: signature,
	}, nil
}

func (s *SignerServer) SignProofOfPossession(ctx context.Context, in *signer.SignProofOfPossessionRequest) (*signer.SignProofOfPossessionResponse, error) {
	signature, err := s.sign(ctx, in.Message, &popDst)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	return &signer.SignProofOfPossessionResponse{
		Signature: signature,
	}, nil
}
