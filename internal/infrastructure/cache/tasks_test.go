package cache

import (
	"context"
	"log/slog"
	"task_tracker/internal/domain"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestUnreachableRedisReportsAnUnknownVersion(t *testing.T) {
	c := NewTasks(deadClient(t), time.Minute, slog.New(slog.DiscardHandler))
	f := domain.TaskFilter{TeamID: 1, Limit: 20}

	_, version, ok := c.GetList(context.Background(), f)

	assert.False(t, ok)
	assert.EqualValues(t, versionUnknown, version)
	assert.NotPanics(t, func() { c.SetList(context.Background(), f, version, nil) })
	assert.NotPanics(t, func() { c.InvalidateTeam(context.Background(), f.TeamID) })
}

func deadClient(t *testing.T) *redis.Client {
	rdb := NewRedis(Config{
		Addr: "127.0.0.1:1", DialTimeout: time.Second, MaxRetries: 1, DialerRetries: 1,
	})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}
