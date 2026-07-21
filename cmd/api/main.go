package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"task_tracker/internal"
	"task_tracker/internal/infrastructure/closer"
	"task_tracker/internal/infrastructure/logger"
)

func main() {
	cfg, err := internal.NewConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config validate:", err)
		os.Exit(1)
	}
	log := logger.NewLog(cfg.Env, cfg.AppName, cfg.AppVersion)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	c := closer.New(log, cfg.Shutdown)

	app, err := internal.NewApp(ctx, c, cfg, log)
	if err != nil {
		cancel()
		log.Error("startup failed", slog.Any("error", err))
		c.ShutDown()
		os.Exit(1)
	}
	err = app.Run(ctx)
	cancel()
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Error("fatal", slog.Any("error", err))
		app.ShutDown()
		os.Exit(1)
	}
	app.ShutDown()
	log.Info("app gracefully stopped")
}
