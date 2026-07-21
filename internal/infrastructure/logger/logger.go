package logger

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

const (
	envLocal = "local"
)

func NewLog(env, appName, version string) *slog.Logger {
	var log *slog.Logger
	if env == envLocal {
		log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	} else {
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: replaceAttr}))
	}
	return log.With(slog.String("app_name", appName),
		slog.String("app_version", version),
		slog.String("env", env))
}

func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) > 0 {
		return a
	}
	switch a.Key {
	case slog.MessageKey:
		a.Key = "event"
	case slog.TimeKey:
		a.Key = "timestamp"
		if t, ok := a.Value.Any().(time.Time); ok {
			a.Value = slog.StringValue(t.UTC().Format("2006-01-02T15:04:05.000Z"))
		}
	case slog.LevelKey:
		if l, ok := a.Value.Any().(slog.Level); ok {
			a.Value = slog.StringValue(strings.ToLower(l.String()))
		}
	}
	return a
}
