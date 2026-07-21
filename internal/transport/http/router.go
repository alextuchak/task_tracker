package http

import (
	"net/http"
	"task_tracker/internal/infrastructure/health"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter(h *health.Health) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/livez", livezHandler)
	r.Get("/readyz", readyzHandler(h))

	r.Route("/api/v1", func(r chi.Router) {
		// handlers are mounted here as features land (auth, teams, tasks)
	})
	return r
}
