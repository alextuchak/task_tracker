package http

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/analytics"
	"task_tracker/internal/transport/http/auth"
	"task_tracker/internal/transport/http/comments"
	"task_tracker/internal/transport/http/middleware"
	"task_tracker/internal/transport/http/tasks"
	"task_tracker/internal/transport/http/teams"

	_ "task_tracker/docs" // generated swagger spec

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

// useObservability keeps Recoverer innermost: it turns a panic into a 500
// before the outer two record the request, so a panicking handler shows up as
// a served error instead of vanishing from both the metrics and the log.
func useObservability(r chi.Router, log *slog.Logger) {
	r.Use(middleware.Metrics())
	r.Use(middleware.Logging(log))
	r.Use(recoverer(log))
}

// recoverer replaces chi's, which prints the panic as coloured plain text to
// stderr — the one stream this service does not read.
func recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rvr := recover()
				if rvr == nil {
					return
				}
				if err, ok := rvr.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rvr)
				}
				log.Error("handler panicked",
					slog.String("method", r.Method), slog.String("path", r.URL.Path),
					slog.Any("panic", rvr), slog.String("stack", string(debug.Stack())))
				w.WriteHeader(http.StatusInternalServerError)
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// a task description tops out at 10KiB, and JSON escaping cannot inflate that
// past this; nothing else the API accepts comes close
const maxRequestBody = 64 << 10

func NewRouter(log *slog.Logger, h *health.Health, authSvc *service.Auth,
	teamsSvc *service.Teams, tasksSvc *service.Tasks, analyticsSvc *service.Analytics,
	commentsSvc *service.Comments,
	parser middleware.TokenParser, userLimiter, ipLimiter middleware.RateLimiter,
	trustedNets []*net.IPNet,
) http.Handler {
	r := chi.NewRouter()
	useObservability(r, log)

	r.Get("/livez", livezHandler)
	r.Get("/readyz", readyzHandler(h, log))
	r.Get("/swagger/*", httpSwagger.Handler(httpSwagger.URL("/swagger/doc.json")))
	r.Handle("/metrics", promhttp.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.LimitBody(maxRequestBody))
		r.Group(func(r chi.Router) {
			r.Use(middleware.RateLimitByIP(ipLimiter, trustedNets))
			r.Mount("/", auth.Routes(authSvc))
		})

		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(parser))
			r.Use(middleware.RateLimit(userLimiter))
			r.Get("/me", auth.Me(authSvc))
			r.Mount("/teams", teams.Routes(teamsSvc))
			r.Mount("/tasks", tasks.Routes(tasksSvc))
			r.Mount("/comments", comments.Routes(commentsSvc))
			r.Mount("/analytics", analytics.Routes(analyticsSvc))
		})
	})
	return r
}
