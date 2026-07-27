package outbox

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	sentTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_sent_total",
		Help: "Emails delivered and settled; deliveries lost to a failed settlement are counted in outbox_tick_errors_total instead",
	})
	failedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_failed_total",
		Help: "Emails moved to the dead-letter state after exhausting retries",
	})
	oldestPendingSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_oldest_pending_seconds",
		Help: "Age of the oldest pending email in seconds (0 when the queue is empty)",
	})
	sendErrorsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "outbox_send_errors_total",
		Help: "Batch sends that returned an error, by reason: provider, budget, shutdown, paused, mismatch",
	}, []string{"reason"})
	tickErrorsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_tick_errors_total",
		Help: "Ticks whose transaction failed, leaving already-delivered rows to be reclaimed; the dedup layer suppresses the resend unless it is also down",
	})
)
