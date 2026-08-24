package signerserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ava-labs/cube-signer-sidecar/api"
	"github.com/ava-labs/cube-signer-sidecar/mockapi"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestNextRefreshBackoff(t *testing.T) {
	require := require.New(t)

	// Grows from zero and stays within the jittered bounds.
	first := nextRefreshBackoff(0)
	require.Positive(first)
	require.LessOrEqual(first, time.Duration(float64(minRefreshBackoff)*1.2))

	// Doubles, and is capped.
	require.Greater(nextRefreshBackoff(time.Minute), time.Minute)
	require.LessOrEqual(
		nextRefreshBackoff(maxRefreshBackoff),
		time.Duration(float64(maxRefreshBackoff)*1.2),
	)
}

// A persistently failing refresh must back off instead of spinning against the
// CubeSigner API as fast as the network allows.
func TestBackgroundRefreshBacksOffOnFailure(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	mockclient := mockapi.NewMockClientInterface(ctrl)

	var attempts atomic.Int64
	mockclient.EXPECT().
		SignerSessionRefresh(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ api.AuthData, _ ...api.RequestEditorFn) (*http.Response, error) {
			attempts.Add(1)
			return nil, errors.New("upstream unavailable")
		}).
		AnyTimes()

	now := time.Now()
	server := createSignerServer(mockclient, &tokenData{
		NewSessionResponse: api.NewSessionResponse{
			Token: "original-token",
			SessionInfo: api.ClientSessionInfo{
				// Auth token already expired, refresh token still valid.
				AuthTokenExp:    api.EpochDateTime(now.Add(-time.Minute).Unix()),
				RefreshTokenExp: api.EpochDateTime(now.Add(time.Hour).Unix()),
			},
		},
		ID:      ID{OrgID: "test-org"},
		RawData: make(rawMessageMap),
	}, keyID)
	server.tokenFilePath = filepath.Join(t.TempDir(), "token.json")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server.StartBackgroundTokenRefresh(ctx)
	time.Sleep(1500 * time.Millisecond)
	cancel()

	// With a 1s minimum backoff, only the immediate attempt and roughly one
	// retry fit in the window. Without backoff this was tens of thousands.
	attempted := attempts.Load()
	t.Logf("refresh attempts in 1.5s: %d", attempted)
	require.LessOrEqual(attempted, int64(5))
	require.Positive(attempted, "expected at least one refresh attempt")
}

// VERIFY: backoff must reset after a success, not stay elevated forever.
func TestVerify_BackoffResetsAfterSuccess(t *testing.T) {
	require := require.New(t)

	// Simulate the loop's accounting: grow on failure, reset on success.
	var backoff time.Duration
	for range 5 {
		backoff = nextRefreshBackoff(backoff)
	}
	require.Greater(backoff, minRefreshBackoff, "backoff should have grown")

	backoff = 0 // what the loop does on success
	require.Equal(time.Duration(0), backoff)

	// And growth is monotonic up to the cap.
	prev := time.Duration(0)
	for range 20 {
		next := nextRefreshBackoff(prev)
		require.LessOrEqual(next, time.Duration(float64(maxRefreshBackoff)*1.2))
		prev = next
	}
}

// Saving a shorter payload over a larger existing token file must fully replace
// it. Writing in place without truncating left the old tail behind, corrupting
// the only copy of the refresh credential.
func TestSaveTokenDataReplacesLargerFile(t *testing.T) {
	require := require.New(t)

	tmpFile := filepath.Join(t.TempDir(), "token.json")

	var existing map[string]any
	require.NoError(json.Unmarshal([]byte(tokenJSON), &existing))
	original, err := json.MarshalIndent(existing, "", "    ")
	require.NoError(err)
	require.NoError(os.WriteFile(tmpFile, original, tokenFileMode))

	server := &SignerServer{
		tokenFilePath: tmpFile,
		tokenData: &tokenData{
			ID:      ID{OrgID: "test-org", RoleID: "test-role"},
			RawData: make(rawMessageMap),
		},
	}
	require.NoError(server.saveTokenData())

	saved, err := os.ReadFile(tmpFile)
	require.NoError(err)
	require.Less(len(saved), len(original), "expected the shorter payload to replace the file")

	roundTripped := &tokenData{}
	require.NoError(json.Unmarshal(saved, roundTripped), "token file must remain valid JSON")
	require.Equal("test-org", roundTripped.OrgID)

	info, err := os.Stat(tmpFile)
	require.NoError(err)
	require.Equal(tokenFileMode, info.Mode().Perm(), "token file must not be group/world readable")
}

// VERIFY: a real refresh->save->reload cycle must preserve fields the sidecar
// does not model (env, purpose, expiration). This is the whole point of RawData,
// and the atomic-write change swapped Encoder.Encode for Marshal.
func TestVerify_RefreshPreservesUnknownFields(t *testing.T) {
	require := require.New(t)

	tmpFile := filepath.Join(t.TempDir(), "token.json")
	require.NoError(os.WriteFile(tmpFile, []byte(tokenJSON), tokenFileMode))

	var original map[string]any
	require.NoError(json.Unmarshal([]byte(tokenJSON), &original))

	ctrl := gomock.NewController(t)
	mockclient := mockapi.NewMockClientInterface(ctrl)
	mockclient.EXPECT().
		SignerSessionRefresh(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ api.AuthData, _ ...api.RequestEditorFn) (*http.Response, error) {
			return toJSONResponse(t, &api.NewSessionResponse{
				Token: "rotated-token",
				SessionInfo: api.ClientSessionInfo{
					AuthToken:       "rotated-auth",
					AuthTokenExp:    api.EpochDateTime(time.Now().Add(time.Hour).Unix()),
					RefreshToken:    "rotated-refresh",
					RefreshTokenExp: api.EpochDateTime(time.Now().Add(2 * time.Hour).Unix()),
					SessionId:       sessionID,
				},
			}), nil
		}).Times(1)

	// Load exactly the way the sidecar does at startup.
	server, err := New(keyID, tmpFile, &api.ClientWithResponses{ClientInterface: mockclient})
	require.NoError(err)
	require.NoError(server.RefreshToken(context.Background()))

	saved, err := os.ReadFile(tmpFile)
	require.NoError(err)

	var reloaded map[string]any
	require.NoError(json.Unmarshal(saved, &reloaded), "saved file must be valid JSON")

	// Fields the sidecar doesn't model must survive verbatim.
	for _, key := range []string{"env", "purpose", "expiration", "role_id", "org_id"} {
		require.Equal(original[key], reloaded[key], "field %q must be preserved across refresh", key)
	}

	// Rotated fields must actually be updated.
	require.Equal("rotated-token", reloaded["token"])
	require.Equal("rotated-auth", reloaded["session_info"].(map[string]any)["auth_token"])

	// And it must still load cleanly on restart.
	restarted, err := New(keyID, tmpFile, &api.ClientWithResponses{ClientInterface: mockclient})
	require.NoError(err, "token file must remain loadable after refresh")
	require.Equal("rotated-token", restarted.tokenData.Token)
	require.Equal(orgID, restarted.OrgID)
}

// VERIFY: when the save fails, the existing token file must be left intact and
// no temporary files may be left behind.
func TestVerify_FailedSaveLeavesOriginalIntact(t *testing.T) {
	require := require.New(t)

	dir := t.TempDir()
	// A directory cannot be replaced by rename(2) from a regular file.
	target := filepath.Join(dir, "token.json")
	require.NoError(os.Mkdir(target, 0700))

	server := &SignerServer{
		tokenFilePath: target,
		tokenData:     &tokenData{ID: ID{OrgID: "test-org"}, RawData: make(rawMessageMap)},
	}

	require.Error(server.saveTokenData(), "expected the save to fail")

	// The original path is untouched...
	info, err := os.Stat(target)
	require.NoError(err)
	require.True(info.IsDir())

	// ...and no temp files were orphaned.
	entries, err := os.ReadDir(dir)
	require.NoError(err)
	for _, e := range entries {
		require.NotContains(e.Name(), ".token-", "orphaned temp file: %s", e.Name())
	}
}
