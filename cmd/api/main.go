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
	"task_tracker/internal/infrastructure/lifecycle"
	"task_tracker/internal/infrastructure/logger"
)

// @title           Task Tracker API
// @version         1.0
// @description     Сервис управления задачами с командной работой и историей изменений.
// @BasePath        /api/v1

// @securityDefinitions.apikey BearerAuth
// @in   header
// @name Authorization
// @description JWT в формате: Bearer {token}

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
	c := lifecycle.NewCloser(log, cfg.Shutdown)

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
