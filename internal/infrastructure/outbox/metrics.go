package outbox

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	sentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_sent_total",
		Help: "Emails delivered by the outbox relay",
	})
	failedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_failed_total",
		Help: "Emails moved to the dead-letter state after exhausting retries",
	})
	oldestPendingSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_oldest_pending_seconds",
		Help: "Age of the oldest pending email in seconds (0 when the queue is empty)",
	})
)
