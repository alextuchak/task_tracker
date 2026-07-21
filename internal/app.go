package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"task_tracker/internal/infrastructure/cache"
	"task_tracker/internal/infrastructure/closer"
	"task_tracker/internal/infrastructure/health"
	"task_tracker/internal/infrastructure/persistence"
	"task_tracker/internal/infrastructure/starter"

	transport "task_tracker/internal/transport/http"
)

func NewApp(ctx context.Context, c *closer.Closer, cfg *Config, log *slog.Logger) (*App, error) {
	st := starter.New(log, cfg.Startup)

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

	srv := &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      transport.NewRouter(h),
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
	closer *closer.Closer
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
