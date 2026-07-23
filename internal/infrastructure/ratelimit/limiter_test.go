package ratelimit

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestAllowFailsOpenWhenRedisDown(t *testing.T) {
	dead := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond})
	l := New(dead, Config{Requests: 1, Window: time.Minute}, slog.New(slog.DiscardHandler))

	allowed, retryAfter := l.Allow(context.Background(), "user:42")

	assert.True(t, allowed)
	assert.Zero(t, retryAfter)
}
