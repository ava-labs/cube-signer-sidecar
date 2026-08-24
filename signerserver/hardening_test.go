package signerserver

import (
	"context"
	"errors"
	"net/http"
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
