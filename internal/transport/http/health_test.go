package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"task_tracker/internal/infrastructure/health"
	"testing"
	"time"
)

func TestReadyzNotReady(t *testing.T) {
	h := health.New(health.Config{CheckTimeout: time.Second})

	rec := httptest.NewRecorder()
	readyzHandler(h)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestReadyzReady(t *testing.T) {
	h := health.New(health.Config{CheckTimeout: time.Second})
	h.SetReady()

	rec := httptest.NewRecorder()
	readyzHandler(h)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestReadyzInfraDown(t *testing.T) {
	h := health.New(health.Config{CheckTimeout: time.Second})
	h.AddCheck(func(ctx context.Context) error { return errors.New("db down") })
	h.SetReady()

	rec := httptest.NewRecorder()
	readyzHandler(h)(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestLivezAlwaysOK(t *testing.T) {
	rec := httptest.NewRecorder()
	livezHandler(rec, httptest.NewRequest(http.MethodGet, "/livez", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
