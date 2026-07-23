// Package middleware holds all http middlewares: auth, logging,
// metrics and later rate-limit.
package middleware

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"task_tracker/internal/identity"
	"task_tracker/internal/transport/http/httpkit"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type TokenParser interface {
	Parse(raw string) (identity.Principal, error)
}

func Auth(parser TokenParser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || raw == "" {
				httpkit.WriteError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			principal, err := parser.Parse(raw)
			if err != nil {
				httpkit.WriteError(w, http.StatusUnauthorized, "invalid token")
				return
			}
			next.ServeHTTP(w, r.WithContext(identity.WithPrincipal(r.Context(), principal)))
		})
	}
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (allowed bool, retryAfter time.Duration)
}

func RateLimit(limiter RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := identity.FromContext(r.Context())
			if !ok {
				httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
				return
			}
			rejectOrServe(w, r, next, limiter, "user:"+strconv.FormatInt(principal.UserID, 10))
		})
	}
}

func RateLimitByIP(limiter RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			rejectOrServe(w, r, next, limiter, "ip:"+host)
		})
	}
}

func rejectOrServe(w http.ResponseWriter, r *http.Request, next http.Handler,
	limiter RateLimiter, key string,
) {
	allowed, retryAfter := limiter.Allow(r.Context(), key)
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		httpkit.WriteError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	next.ServeHTTP(w, r)
}

var skipObservability = map[string]struct{}{
	"/livez":   {},
	"/readyz":  {},
	"/metrics": {},
}

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total http requests",
	}, []string{"method", "route", "status"})
	requestErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_request_errors_total",
		Help: "Total http responses with 5xx status",
	}, []string{"method", "route", "status"})
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Http request duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})
)

func Metrics() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skipObservability[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = "unmatched"
			}
			status := strconv.Itoa(sw.status)
			requestsTotal.WithLabelValues(r.Method, route, status).Inc()
			if sw.status >= http.StatusInternalServerError {
				requestErrors.WithLabelValues(r.Method, route, status).Inc()
			}
			requestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		})
	}
}

func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := skipObservability[r.URL.Path]; ok {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int("bytes", sw.bytes),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
