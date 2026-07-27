package email

import (
	"context"
	"errors"
	"log/slog"
	"task_tracker/internal/infrastructure/outbox"
	"time"

	"github.com/sony/gobreaker/v2"
)

type Client struct {
	breaker *gobreaker.CircuitBreaker[struct{}]
	log     *slog.Logger
	openFor time.Duration
	timeout time.Duration
}

func NewClient(cfg Config, log *slog.Logger) *Client {
	settings := gobreaker.Settings{
		Name:    "email-service",
		Timeout: cfg.OpenFor,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= cfg.MaxFailures
		},
	}
	return &Client{
		breaker: gobreaker.NewCircuitBreaker[struct{}](settings),
		log:     log,
		openFor: cfg.OpenFor,
		timeout: cfg.Timeout,
	}
}

func (c *Client) SendBatch(ctx context.Context, msgs []outbox.Message) ([]outbox.SendResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, err := c.breaker.Execute(func() (struct{}, error) {
		c.log.Info("email batch delivered (mock)", slog.Int("count", len(msgs)))
		return struct{}{}, nil
	})
	if errors.Is(err, gobreaker.ErrOpenState) {
		return nil, &SendError{RetryAfter: c.openFor}
	}
	if err != nil {
		return nil, err
	}
	return make([]outbox.SendResult, len(msgs)), nil
}

type SendError struct {
	RetryAfter time.Duration
}

func (e *SendError) Error() string                 { return "email service unavailable" }
func (e *SendError) RetryAfterHint() time.Duration { return e.RetryAfter }
