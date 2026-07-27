package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"task_tracker/internal/identity"
	"task_tracker/internal/infrastructure/cache"
	"task_tracker/internal/infrastructure/email"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/infrastructure/lifecycle"
	"task_tracker/internal/infrastructure/outbox"
	"task_tracker/internal/infrastructure/persistence"
	"task_tracker/internal/infrastructure/ratelimit"
	"task_tracker/internal/service"

	transport "task_tracker/internal/transport/http"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
	"github.com/avito-tech/go-transaction-manager/trm/v2/manager"
)

func NewApp(ctx context.Context, c *lifecycle.Closer, cfg *Config, log *slog.Logger) (*App, error) {
	st := lifecycle.NewStarter(log, cfg.Startup)

	db, err := persistence.NewMySQL(cfg.MySQL)
	if err != nil {
		return nil, fmt.Errorf("mysql: %w", err)
	}
	c.AddClose(func(context.Context) error { return db.Close() })
	st.AddPing(db.PingContext)

	rdb := cache.NewRedis(cfg.Redis)
	c.AddClose(func(context.Context) error { return rdb.Close() })
	st.AddPing(func(ctx context.Context) error {
		if err := rdb.Ping(ctx).Err(); err != nil {
			log.Warn("redis unavailable at startup, degrading gracefully", slog.Any("error", err))
		}
		return nil
	})

	if err := st.Start(ctx); err != nil {
		return nil, fmt.Errorf("startup pings: %w", err)
	}

	h := health.New(cfg.Health)
	h.AddCheck(func(ctx context.Context) error { return db.PingContext(ctx) })

	getter := trmsql.DefaultCtxGetter
	trManager := manager.Must(trmsql.NewDefaultFactory(db))

	idp := identity.NewProvider(cfg.Auth)
	userRepo := persistence.NewUserRepo(db, getter)
	authService := service.NewAuth(userRepo, idp)
	teamRepo := persistence.NewTeamRepo(db, getter)
	outboxRepo := persistence.NewOutboxRepo(db, getter)
	authz := service.NewAuthorizer(userRepo, teamRepo)
	teamsService := service.NewTeams(teamRepo, userRepo, outboxRepo, trManager, authz)
	tasksCache := cache.NewTasks(rdb, cfg.Redis.TasksTTL, log)
	taskRepo := persistence.NewTaskRepo(db, getter)
	tasksService := service.NewTasks(taskRepo, teamRepo, tasksCache, trManager, authz)
	analyticsService := service.NewAnalytics(persistence.NewAnalyticsRepo(db, getter), authz)
	commentsService := service.NewComments(persistence.NewCommentRepo(db, getter), taskRepo, trManager, authz)
	userLimiter := ratelimit.New(rdb, cfg.RateLimit, log)
	ipLimiter := ratelimit.New(rdb, cfg.RateLimitPublic, log)

	dedup := cache.NewDedup(rdb, cfg.Outbox.DedupTTL, log)
	relay := outbox.NewRelay(outboxRepo, email.NewClient(cfg.Email, log), dedup, trManager, cfg.Outbox, log)
	go relay.Run(ctx)
	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      transport.NewRouter(log, h, authService, teamsService, tasksService, analyticsService, commentsService, idp, userLimiter, ipLimiter, cfg.RateLimitPublic.TrustedNets()),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}
	c.AddDrain(func(ctx context.Context) error { return srv.Shutdown(ctx) })

	return &App{srv: srv, health: h, closer: c, log: log}, nil
}

type App struct {
	srv    *http.Server
	health *health.Health
	closer *lifecycle.Closer
	log    *slog.Logger
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// readiness flips to ready only after the listener is up and serving
	a.health.SetReady()
	a.log.Info("task-tracker started", slog.String("addr", a.srv.Addr))

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	}
}

func (a *App) ShutDown() {
	// first step of graceful shutdown: readiness answers 503 immediately,
	// kubernetes drops the pod from endpoints, then drain waits for in-flight
	a.health.SetShuttingDown()
	a.closer.ShutDown()
}
