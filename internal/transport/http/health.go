package http

import (
	"net/http"
	"task_tracker/internal/infrastructure/health"
)

// livezHandler only proves the process is alive — never gated by the ready
// flag, otherwise kubernetes would restart the pod during graceful shutdown.
func livezHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// readyzHandler answers 503 as soon as readiness fails so kubernetes drops
// the pod from endpoints; the actual logic lives in the health package.
func readyzHandler(h *health.Health) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.CheckReadiness(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}
