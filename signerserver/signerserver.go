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
	"strings"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/cubist-signer/api"
)

var popDst = base64.StdEncoding.EncodeToString(bls.CiphersuiteProofOfPossession.Bytes())

type TokenData struct {
	api.NewSessionResponse
	OrgID  string `json:"org_id"`
	RoleID string `json:"role_id"`
}

type SignerServer struct {
	signer.UnimplementedSignerServer
	OrgId     string
	KeyId     string
	Client    *api.ClientWithResponses
	Session   *api.NewSessionResponse
	publicKey []byte
}

func New(keyId string, tokenData *TokenData, client *api.ClientWithResponses) *SignerServer {
	return &SignerServer{
		OrgId:   tokenData.OrgID,
		KeyId:   keyId,
		Client:  client,
		Session: &tokenData.NewSessionResponse,
	}
}

func (s *SignerServer) AddAuthHeaderFn() api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", s.Session.Token)
		return nil
	}
}

func (s *SignerServer) RefreshToken() error {
	authData := &api.AuthData{
		EpochNum:   s.Session.SessionInfo.Epoch,
		EpochToken: s.Session.SessionInfo.EpochToken,
		OtherToken: s.Session.SessionInfo.RefreshToken,
	}

	res, err := s.Client.SignerSessionRefreshWithResponse(context.Background(), s.OrgId, *authData, s.AddAuthHeaderFn())
	if err != nil {
		return err
	}

	if res.JSON200 == nil {
		return fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	s.Session = res.JSON200
	return nil
}

func (s *SignerServer) PublicKey(ctx context.Context, in *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	if s.publicKey != nil {
		publicKeyRes := &signer.PublicKeyResponse{
			PublicKey: s.publicKey,
		}

		return publicKeyRes, nil
	}

	rsp, err := s.Client.GetKeyInOrg(ctx, s.OrgId, s.KeyId, s.AddAuthHeaderFn())
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

	res, err := s.Client.BlobSignWithResponse(ctx, s.OrgId, s.KeyId, *blobSignReq, s.AddAuthHeaderFn())
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

func (s *SignerServer) SignProofOfPossession(ctx context.Context, in *signer.SignProofOfPossessionRequest) (*signer.SignProofOfPossessionResponse, error) {
	signature, err := s.sign(ctx, in.Message, &popDst)
	if err != nil {
		return nil, err
	}

	signatureRes := &signer.SignProofOfPossessionResponse{
		Signature: signature,
	}

	return signatureRes, nil
}
