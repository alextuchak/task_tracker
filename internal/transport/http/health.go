package http

import (
	"errors"
	"log/slog"
	"net/http"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/transport/http/httpkit"
)

// livezHandler only proves the process is alive — never gated by the ready
// flag, otherwise kubernetes would restart the pod during graceful shutdown.
func livezHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func readyzHandler(h *health.Health, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h.CheckReadiness(r.Context())
		switch {
		case err == nil:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case errors.Is(err, health.ErrStarting), errors.Is(err, health.ErrShuttingDown):
			httpkit.WriteError(w, http.StatusServiceUnavailable, err.Error())
		default:
			log.Error("readiness check failed", slog.Any("error", err))
			httpkit.WriteError(w, http.StatusServiceUnavailable, "not ready")
		}
	}
}
