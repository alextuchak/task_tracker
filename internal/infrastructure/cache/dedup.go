package cache

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"
)

var dedupUnavailable = promauto.NewCounter(prometheus.CounterOpts{
	Name: "outbox_dedup_unavailable_total",
	Help: "Times the dedup store was unreachable and the relay sent without it",
})

func NewDedup(rdb *redis.Client, ttl time.Duration, log *slog.Logger) *Dedup {
	return &Dedup{rdb: rdb, ttl: ttl, log: log}
}

type Dedup struct {
	rdb *redis.Client
	log *slog.Logger
	ttl time.Duration
}

func (d *Dedup) WasSent(ctx context.Context, id int64) bool {
	n, err := d.rdb.Exists(ctx, "outbox:sent:"+strconv.FormatInt(id, 10)).Result()
	if err != nil {
		if isCancelled(err) {
			return false
		}
		dedupUnavailable.Inc()
		d.log.Warn("outbox dedup unavailable, sending without it",
			slog.Int64("id", id), slog.Any("error", err))
		return false
	}
	return n > 0
}

func (d *Dedup) MarkSent(ctx context.Context, id int64) {
	if err := d.rdb.Set(ctx, "outbox:sent:"+strconv.FormatInt(id, 10), 1, d.ttl).Err(); err != nil {
		if isCancelled(err) {
			return
		}
		dedupUnavailable.Inc()
		d.log.Warn("outbox dedup mark failed, delivery may repeat",
			slog.Int64("id", id), slog.Any("error", err))
	}
}
