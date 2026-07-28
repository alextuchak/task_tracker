package ratelimit

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var unavailableTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "ratelimit_unavailable_total",
	Help: "Requests let through because the limiter store was unreachable — while this climbs, nothing is rate limited",
})
