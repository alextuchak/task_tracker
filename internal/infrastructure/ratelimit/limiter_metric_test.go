package ratelimit

import (
	"context"
	"log/slog"
	"task_tracker/internal/infrastructure/cache"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnavailableStoreIsCounted(t *testing.T) {
	dead := cache.NewRedis(cache.Config{
		Addr: "127.0.0.1:1", DialTimeout: time.Second, MaxRetries: 1, DialerRetries: 1,
	})
	t.Cleanup(func() { _ = dead.Close() })
	l := New(dead, Config{Requests: 1, Window: time.Minute}, slog.New(slog.DiscardHandler))
	before := testutil.ToFloat64(unavailableTotal)

	allowed, _ := l.Allow(context.Background(), "user:42")

	require.True(t, allowed)
	assert.Equal(t, before+1, testutil.ToFloat64(unavailableTotal))
}

func TestCancelledCallerIsNotAnOutage(t *testing.T) {
	dead := cache.NewRedis(cache.Config{
		Addr: "127.0.0.1:1", DialTimeout: time.Second, MaxRetries: 1, DialerRetries: 1,
	})
	t.Cleanup(func() { _ = dead.Close() })
	l := New(dead, Config{Requests: 1, Window: time.Minute}, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := testutil.ToFloat64(unavailableTotal)

	allowed, _ := l.Allow(ctx, "user:42")

	require.True(t, allowed, "a limiter that cannot answer must let the request through")
	assert.Equal(t, before, testutil.ToFloat64(unavailableTotal))
}
