package http

import (
	"log/slog"
	"net/http"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/analytics"
	"task_tracker/internal/transport/http/auth"
	"task_tracker/internal/transport/http/middleware"
	"task_tracker/internal/transport/http/tasks"
	"task_tracker/internal/transport/http/teams"

	_ "task_tracker/docs" // generated swagger spec

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

func NewRouter(log *slog.Logger, h *health.Health, authSvc *service.Auth,
	teamsSvc *service.Teams, tasksSvc *service.Tasks, analyticsSvc *service.Analytics,
	parser middleware.TokenParser, userLimiter, ipLimiter middleware.RateLimiter,
) http.Handler {
	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(middleware.Metrics())
	r.Use(middleware.Logging(log))

	r.Get("/livez", livezHandler)
	r.Get("/readyz", readyzHandler(h))
	r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimitByIP(ipLimiter))
			r.Mount("/", auth.Routes(authSvc))
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(parser))
			r.Use(middleware.RateLimit(userLimiter))
			r.Get("/me", auth.Me(authSvc))
			r.Mount("/teams", teams.Routes(teamsSvc))
			r.Mount("/tasks", tasks.Routes(tasksSvc))
			r.Mount("/analytics", analytics.Routes(analyticsSvc))
		})
	})
	return r
}
