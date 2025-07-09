//go:generate go run go.uber.org/mock/mockgen -package=mockapi -source=../api/client.go -destination=../mockapi/mockclient.go

package signerserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/cubist-signer/api"
	"github.com/ava-labs/cubist-signer/mockapi"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

var (
	testTokenData = &tokenData{
		NewSessionResponse: api.NewSessionResponse{
			Token: "test-token",
		},
		ID: ID{
			OrgID:  "test-org",
			RoleID: "test-role",
		},
		KeyID: KeyID{"test-key"},
	}
)

func TestSignerServerSaveTokenData(t *testing.T) {
	require := require.New(t)

	tempDir := t.TempDir()
	tmpFile := filepath.Join(tempDir, "token.json")
	file, err := os.OpenFile(tmpFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	require.NoError(err)
	_, err = file.Write([]byte("{}"))
	require.NoError(err)
	require.NoError(file.Close())

	server := &SignerServer{
		tokenFilePath: tmpFile,
		tokenData: &tokenData{
			ID:      testTokenData.ID,
			RawData: make(rawMessageMap),
		},
	}

	require.NoError(server.saveTokenData())

	savedData := &tokenData{}
	file, err = os.Open(tmpFile)
	require.NoError(err)
	require.NoError(json.NewDecoder(file).Decode(savedData))
	require.NoError(file.Close())

	require.Equal(savedData.OrgID, testTokenData.OrgID)
	require.Equal(savedData.RoleID, testTokenData.RoleID)
}

func TestSignerServerGetPublicKey(t *testing.T) {
	require := require.New(t)
	localsigner, err := localsigner.New()
	pkBytes := bls.PublicKeyToCompressedBytes(localsigner.PublicKey())
	require.NoError(err)
	ctrl := gomock.NewController(t)
	mockclient := mockapi.NewMockClientInterface(ctrl)

	mockclient.
		EXPECT().
		GetKeyInOrg(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, orgID string, keyID string, reqEditor api.RequestEditorFn) (*http.Response, error) {
			require.Equal(orgID, testTokenData.OrgID)
			require.Equal(keyID, keyID)

			req := newRequest()
			err := reqEditor(context.Background(), req)
			require.NoError(err)
			require.Equal(req.Header.Get("Authorization"), testTokenData.Token)

			keyInfo := &KeyInfo{
				PublicKey: "0x" + hex.EncodeToString(pkBytes),
			}

			return toJSONResponse(t, keyInfo), nil
		}).
		Times(1)

	signerServer := createSignerServer(mockclient, testTokenData)

	res, err := signerServer.PublicKey(context.Background(), &signer.PublicKeyRequest{})
	require.NoError(err)
	require.Equal(res.PublicKey, pkBytes)

	// make sure the public key is cached
	res, err = signerServer.PublicKey(context.Background(), &signer.PublicKeyRequest{})
	require.NoError(err)
	require.Equal(res.PublicKey, pkBytes)
}

func TestSignerServerSign(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	mockclient := mockapi.NewMockClientInterface(ctrl)

	localsigner, err := localsigner.New()
	require.NoError(err)

	mockclient.
		EXPECT().
		BlobSign(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, orgID string, keyID string, reqBody api.BlobSignRequest, reqEditor api.RequestEditorFn) (*http.Response, error) {
			require.Equal(orgID, testTokenData.OrgID)
			require.Equal(keyID, keyID)
			require.Nil(reqBody.BlsDst)

			req := newRequest()
			err := reqEditor(context.Background(), req)
			require.NoError(err)
			require.Equal(req.Header.Get("Authorization"), testTokenData.Token)

			msg, err := base64.StdEncoding.DecodeString(reqBody.MessageBase64)
			require.NoError(err)

			sig, err := localsigner.Sign(msg)
			require.NoError(err)

			sigBytes := bls.SignatureToBytes(sig)

			signResponse := &api.SignResponse{
				Signature: "0x" + hex.EncodeToString(sigBytes),
			}

			return toJSONResponse(t, signResponse), nil
		}).
		Times(1)

	signerServer := createSignerServer(mockclient, testTokenData)
	msg := []byte("test-message")

	res, err := signerServer.Sign(context.Background(), &signer.SignRequest{Message: msg})
	require.NoError(err)

	sig, err := bls.SignatureFromBytes(res.Signature)
	require.NoError(err)

	isValid := bls.Verify(localsigner.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestSignerServerSignProofOfPossession(t *testing.T) {
	require := require.New(t)

	ctrl := gomock.NewController(t)
	mockclient := mockapi.NewMockClientInterface(ctrl)

	localsigner, err := localsigner.New()
	require.NoError(err)

	mockclient.
		EXPECT().
		BlobSign(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, orgID string, keyID string, reqBody api.BlobSignRequest, reqEditor api.RequestEditorFn) (*http.Response, error) {
			require.Equal(orgID, testTokenData.OrgID)
			require.Equal(keyID, keyID)
			require.Equal(popDst, *reqBody.BlsDst)

			req := newRequest()
			err = reqEditor(context.Background(), req)
			require.NoError(err)
			require.Equal(req.Header.Get("Authorization"), testTokenData.Token)

			msg, err := base64.StdEncoding.DecodeString(reqBody.MessageBase64)
			require.NoError(err)

			sig, err := localsigner.SignProofOfPossession(msg)
			require.NoError(err)

			sigBytes := bls.SignatureToBytes(sig)

			signResponse := &api.SignResponse{
				Signature: "0x" + hex.EncodeToString(sigBytes),
			}

			return toJSONResponse(t, signResponse), nil
		}).
		Times(1)

	signerServer := createSignerServer(mockclient, testTokenData)
	msg := []byte("test-message")

	res, err := signerServer.SignProofOfPossession(context.Background(), &signer.SignProofOfPossessionRequest{Message: msg})
	require.NoError(err)

	sig, err := bls.SignatureFromBytes(res.Signature)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(localsigner.PublicKey(), sig, msg)
	require.True(isValid)
}

func createSignerServer(mockclient *mockapi.MockClientInterface, tokenData *tokenData) *SignerServer {
	return &SignerServer{
		OrgID:         tokenData.OrgID,
		client:        &api.ClientWithResponses{ClientInterface: mockclient},
		tokenData:     tokenData,
		tokenFilePath: "",
	}
}

func toJSONResponse(t *testing.T, v any) *http.Response {
	t.Helper()
	body, err := json.Marshal(v)
	require.NoError(t, err)

	header := make(http.Header)
	header.Set("Content-Type", "application/json")

	return &http.Response{
		StatusCode: 200,
		Header:     header,
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func newRequest() *http.Request {
	return &http.Request{
		Header: make(http.Header),
	}
}
