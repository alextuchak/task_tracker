package cache

import (
	"context"
	"errors"

	"github.com/redis/go-redis/v9"
)

func isCancelled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func NewRedis(cfg Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		DialTimeout:           cfg.DialTimeout,
		ReadTimeout:           cfg.ReadTimeout,
		WriteTimeout:          cfg.WriteTimeout,
		MaxRetries:            cfg.MaxRetries,
		DialerRetries:         cfg.DialerRetries,
		ContextTimeoutEnabled: true,
	})
}
