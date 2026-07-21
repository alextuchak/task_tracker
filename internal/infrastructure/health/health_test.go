package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckReadinessStartingSkipsChecks(t *testing.T) {
	h := New(Config{CheckTimeout: time.Second})
	checkCalled := false
	h.AddCheck(func(ctx context.Context) error {
		checkCalled = true
		return nil
	})

	err := h.CheckReadiness(context.Background())

	if !errors.Is(err, ErrStarting) {
		t.Fatalf("expected ErrStarting, got %v", err)
	}
	if checkCalled {
		t.Fatal("infra check must not run before startup finishes")
	}
}

func TestCheckReadinessReadyAndChecksPass(t *testing.T) {
	h := New(Config{CheckTimeout: time.Second})
	h.AddCheck(func(ctx context.Context) error { return nil })
	h.SetReady()

	if err := h.CheckReadiness(context.Background()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckReadinessReadyButCheckFails(t *testing.T) {
	h := New(Config{CheckTimeout: time.Second})
	infraErr := errors.New("db down")
	h.AddCheck(func(ctx context.Context) error { return infraErr })
	h.SetReady()

	if err := h.CheckReadiness(context.Background()); !errors.Is(err, infraErr) {
		t.Fatalf("expected infra error, got %v", err)
	}
}

func TestCheckReadinessShuttingDownSkipsChecks(t *testing.T) {
	h := New(Config{CheckTimeout: time.Second})
	checkCalled := false
	h.AddCheck(func(ctx context.Context) error {
		checkCalled = true
		return nil
	})
	h.SetReady()
	h.SetShuttingDown()

	err := h.CheckReadiness(context.Background())

	if !errors.Is(err, ErrShuttingDown) {
		t.Fatalf("expected ErrShuttingDown, got %v", err)
	}
	if checkCalled {
		t.Fatal("infra check must not run during shutdown")
	}
}
