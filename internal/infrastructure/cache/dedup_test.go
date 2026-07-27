package cache

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestDedupFailsOpen(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	d := NewDedup(rdb, time.Hour, slog.New(slog.DiscardHandler))

	assert.False(t, d.WasSent(context.Background(), 1))
	assert.NotPanics(t, func() { d.MarkSent(context.Background(), 1) })
}
