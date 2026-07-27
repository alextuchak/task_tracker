package ratelimit

import (
	"context"
	"log/slog"
	"task_tracker/internal/infrastructure/cache"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAllowFailsOpenWhenRedisDown(t *testing.T) {
	dead := cache.NewRedis(cache.Config{
		Addr: "127.0.0.1:1", DialTimeout: time.Second, MaxRetries: 1, DialerRetries: 1,
	})
	t.Cleanup(func() { _ = dead.Close() })
	l := New(dead, Config{Requests: 1, Window: time.Minute}, slog.New(slog.DiscardHandler))

	allowed, retryAfter := l.Allow(context.Background(), "user:42")

	assert.True(t, allowed)
	assert.Zero(t, retryAfter)
}
