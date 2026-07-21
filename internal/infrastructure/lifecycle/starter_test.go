package lifecycle

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestStartPingsAllRegistered(t *testing.T) {
	s := NewStarter(discard(), StarterConfig{Timeout: time.Second})
	var called atomic.Int32
	s.AddPing(
		func(ctx context.Context) error { called.Add(1); return nil },
		func(ctx context.Context) error { called.Add(1); return nil },
	)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called.Load() != 2 {
		t.Fatalf("expected 2 pings called, got %d", called.Load())
	}
}

func TestStartReturnsFailedPings(t *testing.T) {
	s := NewStarter(discard(), StarterConfig{Timeout: time.Second})
	dbErr := errors.New("mysql down")
	s.AddPing(
		func(ctx context.Context) error { return nil },
		func(ctx context.Context) error { return dbErr },
	)

	if err := s.Start(context.Background()); !errors.Is(err, dbErr) {
		t.Fatalf("expected mysql error, got %v", err)
	}
}

func TestStartNoPings(t *testing.T) {
	s := NewStarter(discard(), StarterConfig{Timeout: time.Second})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("expected nil for empty starter, got %v", err)
	}
}

func TestStartTimeout(t *testing.T) {
	s := NewStarter(discard(), StarterConfig{Timeout: 50 * time.Millisecond})
	s.AddPing(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	if err := s.Start(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
