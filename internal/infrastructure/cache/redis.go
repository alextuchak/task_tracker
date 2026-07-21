package cache

import (
	"github.com/redis/go-redis/v9"
)

// NewRedis only builds the client; liveness is verified by the starter.
func NewRedis(cfg Config) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
}
