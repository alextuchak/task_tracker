package cache

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func deadDedup(t *testing.T) *Dedup {
	t.Helper()
	rdb := NewRedis(Config{
		Addr: "127.0.0.1:1", DialTimeout: time.Second, MaxRetries: 1, DialerRetries: 1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewDedup(rdb, time.Hour, slog.New(slog.DiscardHandler))
}

func TestUnreachableDedupIsCounted(t *testing.T) {
	d := deadDedup(t)
	before := testutil.ToFloat64(dedupUnavailable)

	require.False(t, d.WasSent(context.Background(), 1))
	d.MarkSent(context.Background(), 1)

	assert.Equal(t, before+2, testutil.ToFloat64(dedupUnavailable),
		"a failed mark is the worse half: those deliveries have no dedup entry")
}

func TestCancelledDedupCallIsNotAnOutage(t *testing.T) {
	d := deadDedup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := testutil.ToFloat64(dedupUnavailable)

	require.False(t, d.WasSent(ctx, 1))
	d.MarkSent(ctx, 1)

	assert.Equal(t, before, testutil.ToFloat64(dedupUnavailable))
}
