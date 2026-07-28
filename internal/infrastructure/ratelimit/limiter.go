package ratelimit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

func New(rdb *redis.Client, cfg Config, log *slog.Logger) *Limiter {
	return &Limiter{
		rl: redis_rate.NewLimiter(rdb),
		limit: redis_rate.Limit{
			Rate:   cfg.Requests,
			Period: cfg.Window,
			Burst:  cfg.Requests,
		},
		log: log,
	}
}

type Limiter struct {
	rl    *redis_rate.Limiter
	log   *slog.Logger
	limit redis_rate.Limit
}

func (l *Limiter) Allow(ctx context.Context, key string) (bool, time.Duration) {
	res, err := l.rl.Allow(ctx, "rate:"+key, l.limit)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return true, 0
		}
		unavailableTotal.Inc()
		l.log.Warn("rate limiter unavailable, failing open",
			slog.String("key", key), slog.Any("error", err))
		return true, 0
	}
	if res.Allowed == 0 {
		return false, res.RetryAfter
	}
	return true, 0
}
