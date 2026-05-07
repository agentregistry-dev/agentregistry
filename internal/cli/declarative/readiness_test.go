package declarative

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForHTTPReady_FirstAttemptSucceeds(t *testing.T) {
	var calls int32
	get := func(ctx context.Context, url string) (int, error) {
		atomic.AddInt32(&calls, 1)
		return 200, nil
	}
	err := waitForHTTPReady(context.Background(), "http://x", 1*time.Second, 10*time.Millisecond, get)
	require.NoError(t, err)
	require.EqualValues(t, 1, atomic.LoadInt32(&calls))
}

func TestWaitForHTTPReady_RetriesAfterTransientError(t *testing.T) {
	var calls int32
	get := func(ctx context.Context, url string) (int, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return 0, errors.New("connection refused")
		}
		return 200, nil
	}
	err := waitForHTTPReady(context.Background(), "http://x", 2*time.Second, 10*time.Millisecond, get)
	require.NoError(t, err)
	require.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(3))
}

func TestWaitForHTTPReady_RetriesOn5xx(t *testing.T) {
	var calls int32
	get := func(ctx context.Context, url string) (int, error) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			return 503, nil
		}
		return 204, nil
	}
	err := waitForHTTPReady(context.Background(), "http://x", 1*time.Second, 10*time.Millisecond, get)
	require.NoError(t, err)
	require.GreaterOrEqual(t, atomic.LoadInt32(&calls), int32(2))
}

func TestWaitForHTTPReady_TimeoutWhenNeverReady(t *testing.T) {
	get := func(ctx context.Context, url string) (int, error) {
		return 500, nil
	}
	start := time.Now()
	err := waitForHTTPReady(context.Background(), "http://x", 100*time.Millisecond, 10*time.Millisecond, get)
	require.Error(t, err)
	require.WithinDuration(t, start.Add(100*time.Millisecond), time.Now(), 200*time.Millisecond)
}

func TestIsReady(t *testing.T) {
	require.True(t, isReady(200))
	require.True(t, isReady(204))
	require.True(t, isReady(301))
	require.False(t, isReady(404))
	require.False(t, isReady(500))
	require.False(t, isReady(0))
}
