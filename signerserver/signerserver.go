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
	"github.com/ava-labs/cubist-signer/api"
)

var popDst = base64.StdEncoding.EncodeToString(bls.CiphersuiteProofOfPossession.Bytes())

type SignerServer struct {
	signer.UnimplementedSignerServer
	OrgID         string
	KeyID         string
	client        *api.ClientWithResponses
	tokenData     *tokenData
	tokenFilePath string
	publicKey     []byte
}

func New(keyID string, tokenFilePath string, client *api.ClientWithResponses) (*SignerServer, error) {
	tokenFile, err := os.Open(tokenFilePath)
	if err != nil {
		return nil, err
	}

	var tokenData tokenData
	if err := json.NewDecoder(tokenFile).Decode(&tokenData); err != nil {
		return nil, err
	}

	return &SignerServer{
		OrgID:         tokenData.OrgID,
		KeyID:         keyID,
		client:        client,
		tokenData:     &tokenData,
		tokenFilePath: tokenFilePath,
	}, nil
}

func (s *SignerServer) addAuthHeaderFn() api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", s.tokenData.Token)
		return nil
	}
}

func (s *SignerServer) RefreshToken() error {
	authData := s.tokenData.toAuthData()

	res, err := s.client.SignerSessionRefreshWithResponse(context.Background(), s.OrgID, *authData, s.addAuthHeaderFn())
	if err != nil {
		return err
	}

	if res.JSON200 == nil {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	s.tokenData.NewSessionResponse = *res.JSON200
	return s.saveTokenData()
}

func (s *SignerServer) saveTokenData() error {
	file, err := os.OpenFile(s.tokenFilePath, os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

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

				if waitDuration < 0 {
					refreshExpiryTime := time.Unix(int64(s.tokenData.SessionInfo.RefreshTokenExp), 0)
					if time.Until(refreshExpiryTime) < 0 {
						log.Fatalf("Refresh token expired at %v", refreshExpiryTime)
					}
					waitDuration = 0
				}

				timer := time.NewTimer(waitDuration)
				select {
				case <-ctx.Done():
					timer.Stop()
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
	if s.publicKey != nil {
		publicKeyRes := &signer.PublicKeyResponse{
			PublicKey: s.publicKey,
		}

		return publicKeyRes, nil
	}

	rsp, err := s.client.GetKeyInOrg(ctx, s.OrgID, s.KeyID, s.addAuthHeaderFn())
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

	publicKey, err := hex.DecodeString(res.JSON200.PublicKey[2:])
	if err != nil {
		return nil, err
	}

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
// this code can be removed if cubist fixes the openapi-spec
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

	res, err := s.client.BlobSignWithResponse(ctx, s.OrgID, s.KeyID, *blobSignReq, s.addAuthHeaderFn())
	if err != nil {
		return nil, err
	}

	if res.JSON200 == nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	return hex.DecodeString(res.JSON200.Signature[2:])
}

func (s *SignerServer) Sign(ctx context.Context, in *signer.SignRequest) (*signer.SignResponse, error) {
	signature, err := s.sign(ctx, in.Message, nil)
	if err != nil {
		return nil, err
	}

	return &signer.SignResponse{
		Signature: signature,
	}, nil
}

func (s *SignerServer) SignProofOfPossession(ctx context.Context, in *signer.SignProofOfPossessionRequest) (*signer.SignProofOfPossessionResponse, error) {
	signature, err := s.sign(ctx, in.Message, &popDst)
	if err != nil {
		return nil, err
	}

	return &signer.SignProofOfPossessionResponse{
		Signature: signature,
	}, nil
}
