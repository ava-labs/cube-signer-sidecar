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
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/cube-signer-sidecar/api"
)

const (
	// The token file holds the session's refresh credential, so keep it readable
	// only by the user running the sidecar.
	tokenFileMode os.FileMode = 0600

	// How far ahead of the auth token's expiry to refresh it.
	tokenRefreshLeeway = time.Second

	// Bounds for the exponential backoff applied after a failed refresh, so that
	// a failing upstream isn't hammered until the refresh token expires.
	minRefreshBackoff = time.Second
	maxRefreshBackoff = 5 * time.Minute
)

var popDst = base64.StdEncoding.EncodeToString(bls.CiphersuiteProofOfPossession.Bytes())

type SignerServer struct {
	signer.UnimplementedSignerServer
	OrgID         string
	KeyID         string
	client        *api.ClientWithResponses
	tokenFilePath string

	// mu guards tokenData and publicKey, which are read by concurrent gRPC
	// handlers while the background refresh goroutine writes tokenData.
	mu        sync.RWMutex
	tokenData *tokenData
	publicKey []byte
}

func New(keyID string, tokenFilePath string, client *api.ClientWithResponses) (*SignerServer, error) {
	tokenFile, err := os.Open(tokenFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open token file: %w", err)
	}

	var tokenData tokenData
	if err := json.NewDecoder(tokenFile).Decode(&tokenData); err != nil {
		return nil, fmt.Errorf("failed to decode token data: %w", err)
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
		s.mu.RLock()
		token := s.tokenData.Token
		s.mu.RUnlock()

		req.Header.Set("Authorization", token)
		return nil
	}
}

// authData returns the credentials used to refresh the session.
func (s *SignerServer) authData() api.AuthData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return *s.tokenData.toAuthData()
}

// sessionExpiry returns the expiry times of the auth and refresh tokens.
func (s *SignerServer) sessionExpiry() (authExp time.Time, refreshExp time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return time.Unix(int64(s.tokenData.SessionInfo.AuthTokenExp), 0),
		time.Unix(int64(s.tokenData.SessionInfo.RefreshTokenExp), 0)
}

func (s *SignerServer) RefreshToken(ctx context.Context) error {
	authData := s.authData()

	res, err := s.client.SignerSessionRefreshWithResponse(ctx, s.OrgID, authData, s.addAuthHeaderFn())
	if err != nil {
		return fmt.Errorf("failed to refresh session: %w", err)
	}

	if res.JSON200 == nil {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tokenData.NewSessionResponse = *res.JSON200
	return s.saveTokenDataLocked()
}

func (s *SignerServer) saveTokenData() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveTokenDataLocked()
}

// saveTokenDataLocked replaces the token file atomically. The caller must hold
// s.mu for writing.
//
// The file holds the session's refresh credential and is the only copy of it. A
// truncated or partial write leaves the sidecar unable to start, so serialize
// first, write to a temporary file in the same directory, then rename it into
// place.
func (s *SignerServer) saveTokenDataLocked() error {
	log.Println("Saving token data")

	data, err := json.Marshal(s.tokenData)
	if err != nil {
		return fmt.Errorf("failed to encode token data: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.tokenFilePath), ".token-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temporary token file: %w", err)
	}
	tmpName := tmp.Name()

	// Clean up the temporary file unless it was renamed into place.
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmp.Chmod(tokenFileMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set token file permissions: %w", err)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write token data: %w", err)
	}

	// Ensure the contents reach disk before the rename publishes the file.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync token data: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temporary token file: %w", err)
	}

	if err := os.Rename(tmpName, s.tokenFilePath); err != nil {
		return fmt.Errorf("failed to replace token file: %w", err)
	}
	tmpName = ""

	return nil
}

// nextRefreshBackoff returns the delay before the next refresh attempt, doubling
// up to maxRefreshBackoff with jitter so that many sidecars recovering from the
// same upstream outage don't retry in lockstep.
func nextRefreshBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next < minRefreshBackoff {
		next = minRefreshBackoff
	}
	if next > maxRefreshBackoff {
		next = maxRefreshBackoff
	}

	// Apply +/-20% jitter.
	jitter := 1 + (rand.Float64()*0.4 - 0.2)
	return time.Duration(float64(next) * jitter)
}

func (s *SignerServer) StartBackgroundTokenRefresh(ctx context.Context) {
	go func() {
		var backoff time.Duration

		for {
			authExpiryTime, refreshExpiryTime := s.sessionExpiry()

			waitDuration := time.Until(authExpiryTime) - tokenRefreshLeeway
			if waitDuration < 0 {
				if time.Until(refreshExpiryTime) < 0 {
					log.Fatalf("Refresh token expired at %v", refreshExpiryTime)
				}
				waitDuration = 0
			}

			// Never retry sooner than the backoff a previous failure earned.
			if backoff > waitDuration {
				waitDuration = backoff
			}

			log.Printf("Waiting %s until refreshing token", waitDuration)

			timer := time.NewTimer(waitDuration)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			if err := s.RefreshToken(ctx); err != nil {
				backoff = nextRefreshBackoff(backoff)
				log.Printf("Failed to refresh token (retrying in %s): %v", backoff, err)
				continue
			}

			backoff = 0
		}
	}()
}

func (s *SignerServer) cachedPublicKey() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.publicKey
}

func (s *SignerServer) cachePublicKey(publicKey []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.publicKey = publicKey
}

func (s *SignerServer) PublicKey(ctx context.Context, in *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	log.Println("Serving pubkey request")

	if publicKey := s.cachedPublicKey(); publicKey != nil {
		log.Println("Returning cached pubkey")
		publicKeyRes := &signer.PublicKeyResponse{
			PublicKey: publicKey,
		}

		return publicKeyRes, nil
	}

	rsp, err := s.client.GetKeyInOrg(ctx, s.OrgID, s.KeyID, s.addAuthHeaderFn())
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

	s.cachePublicKey(publicKey)

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

	res, err := s.client.BlobSignWithResponse(ctx, s.OrgID, s.KeyID, *blobSignReq, s.addAuthHeaderFn())
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
