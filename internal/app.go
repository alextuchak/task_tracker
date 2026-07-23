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
	"task_tracker/internal/infrastructure/persistence"
	"task_tracker/internal/service"

	transport "task_tracker/internal/transport/http"
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
	st.AddPing(func(ctx context.Context) error { return rdb.Ping(ctx).Err() })

	if err := st.Start(ctx); err != nil {
		return nil, fmt.Errorf("startup pings: %w", err)
	}

	h := health.New(cfg.Health)
	h.AddCheck(
		func(ctx context.Context) error { return db.PingContext(ctx) },
		func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
	)

	idp := identity.NewProvider(cfg.Auth)
	userRepo := persistence.NewUserRepo(db)
	authService := service.NewAuth(userRepo, idp)
	teamRepo := persistence.NewTeamRepo(db)
	authz := service.NewAuthorizer(userRepo, teamRepo)
	teamsService := service.NewTeams(teamRepo, userRepo, email.NewClient(cfg.Email), authz, log)
	tasksService := service.NewTasks(persistence.NewTaskRepo(db), teamRepo, authz)
	analyticsService := service.NewAnalytics(persistence.NewAnalyticsRepo(db), authz)

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      transport.NewRouter(log, h, authService, teamsService, tasksService, analyticsService, idp),
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
