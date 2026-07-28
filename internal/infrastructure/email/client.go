package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"task_tracker/internal/infrastructure/outbox"
	"time"

	"github.com/sony/gobreaker/v2"
)

var (
	errCallerGone      = errors.New("caller context ended")
	errProviderTimeout = errors.New("provider timed out")
)

type Client struct {
	breaker *gobreaker.CircuitBreaker[struct{}]
	send    func(ctx context.Context, msgs []outbox.Message) error
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
		IsExcluded: func(err error) bool { return errors.Is(err, errCallerGone) },
	}
	c := &Client{
		breaker: gobreaker.NewCircuitBreaker[struct{}](settings),
		log:     log,
		openFor: cfg.OpenFor,
		timeout: cfg.Timeout,
	}
	c.send = c.deliver
	return c
}

func (c *Client) deliver(_ context.Context, msgs []outbox.Message) error {
	c.log.Info("email batch delivered (mock)", slog.Int("count", len(msgs)))
	return nil
}

func (c *Client) SendBatch(ctx context.Context, msgs []outbox.Message) ([]outbox.SendResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(parent, c.timeout)
	defer cancel()

	_, err := c.breaker.Execute(func() (struct{}, error) {
		err := c.send(ctx, msgs)
		switch {
		case err == nil:
			return struct{}{}, nil
		case parent.Err() != nil:
			return struct{}{}, fmt.Errorf("%w: %w", errCallerGone, err)
		case errors.Is(err, context.DeadlineExceeded):
			return struct{}{}, fmt.Errorf("%w after %s", errProviderTimeout, c.timeout)
		}
		return struct{}{}, err
	})
	switch {
	case err == nil:
		return make([]outbox.SendResult, len(msgs)), nil
	case errors.Is(err, errCallerGone):
		return nil, parent.Err()
	}
	return nil, &SendError{RetryAfter: c.openFor, Err: err}
}

type SendError struct {
	Err        error
	RetryAfter time.Duration
}

func (e *SendError) Error() string {
	return fmt.Sprintf("email service unavailable for %s: %v", e.RetryAfter, e.Err)
}

func (e *SendError) Unwrap() error                 { return e.Err }
func (e *SendError) RetryAfterHint() time.Duration { return e.RetryAfter }
