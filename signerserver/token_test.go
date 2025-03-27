package signerserver

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ava-labs/cubist-signer/api"
	"github.com/stretchr/testify/require"
)

const (
	roleID                            = "bls_signer"
	orgID                             = "Org#aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	expiration      int64             = 1774551174
	tokenValue                        = "3d6fd7397:MDhlMWRkMjAtNmMzYy00YjdmLWJmZjgtYTQzNWVkMDk4NWNh.eyJlcG9jaF9udW0iOjEsImVwb2NoX3Rva2VuIjoiVWJCczZURDc0V2ZjOGdCMFp1R1BpNWViOTUwSTdidmlHblk5WjFDTlF4ST0iLCJvdGhlcl90b2tlbiI6InR1eXFQd0xHbCtFZzBjTnU5alNvNlptMHlBM09UUFppQUFabzdOT2V2VUE9In0="
	authToken                         = "tuyqPwLGl+Eg0cNu9jSo6Zm0yA3OTPZiAAZo7NOevUA="
	authTokenExp    api.EpochDateTime = 1743015474
	epoch           int32             = 1
	epochToken                        = "UbBs6TD74Wfc8gB0ZuGPi5eb950I7bviGnY9Z1CNQxI="
	refreshToken                      = "/xV0TjG18/MwpVb63mZPnQKuhPqaQGXj4KEzyzAWlaI="
	refreshTokenExp api.EpochDateTime = 1743101574
	sessionID                         = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

var tokenJSON = `
{
  "org_id": "` + orgID + `",
  "role_id": "` + roleID + `",
  "expiration": ` + fmt.Sprint(expiration) + `,
  "purpose": "Role session for bls_signer with scopes [\"sign:*\"]",
  "token": "` + tokenValue + `",
  "refresh_token": "` + tokenValue + `.` + refreshToken + `",
  "env": {
	"Dev-CubeSignerStack": {
	  "SignerApiRoot": "https://gamma.signer.cubist.dev",
	  "DefaultCredentialRpId": "cubist.dev",
	  "EncExportS3BucketName": null,
	  "DeletedKeysS3BucketName": null
	}
  },
  "session_info": {
	"auth_token": "` + authToken + `",
	"auth_token_exp": ` + fmt.Sprint(authTokenExp) + `,
	"epoch": ` + fmt.Sprint(epoch) + `,
	"epoch_token": "` + epochToken + `",
	"refresh_token": "` + refreshToken + `",
	"refresh_token_exp": ` + fmt.Sprint(refreshTokenExp) + `,
	"session_id": "` + sessionID + `"
  }
}`

func TestTokenDataUnmarshalJSON(t *testing.T) {
	require := require.New(t)

	var token tokenData
	err := json.Unmarshal([]byte(tokenJSON), &token)
	require.NoError(err)

	require.Equal(token.OrgID, orgID)
	require.Equal(token.RoleID, roleID)

	sessionInfo := token.SessionInfo
	require.Equal(sessionInfo.AuthToken, authToken)
	require.Equal(sessionInfo.AuthTokenExp, authTokenExp)
	require.Equal(sessionInfo.Epoch, epoch)
	require.Equal(sessionInfo.EpochToken, epochToken)
	require.Equal(sessionInfo.RefreshToken, refreshToken)
	require.Equal(sessionInfo.RefreshTokenExp, refreshTokenExp)
	require.Equal(sessionInfo.SessionId, sessionID)
}
func TestTokenDataMarshalJSON(t *testing.T) {
	require := require.New(t)

	var token tokenData
	err := json.Unmarshal([]byte(tokenJSON), &token)
	require.NoError(err)

	marshaledJSON, err := json.Marshal(&token)
	require.NoError(err)

	var originalMap, marshaledMap map[string]any
	err = json.Unmarshal([]byte(tokenJSON), &originalMap)
	require.NoError(err)

	err = json.Unmarshal(marshaledJSON, &marshaledMap)
	require.NoError(err)

	require.EqualValues(originalMap, marshaledMap)
}
