package middleware

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"task_tracker/internal/identity"
	"testing"
	"time"
)

type parserMock struct {
	err       error
	principal identity.Principal
}

func (p parserMock) Parse(raw string) (identity.Principal, error) { return p.principal, p.err }

func TestAuthNoHeader(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called without a token")
	})

	rec := httptest.NewRecorder()
	Auth(parserMock{})(next).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthBadToken(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called with an invalid token")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil)
	req.Header.Set("Authorization", "Bearer garbage")

	rec := httptest.NewRecorder()
	Auth(parserMock{err: errors.New("bad token")})(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestAuthPutsUserIDIntoContext(t *testing.T) {
	var gotUserID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := identity.FromContext(r.Context())
		if !ok {
			t.Fatal("expected user id in context")
		}
		gotUserID = p.UserID
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil)
	req.Header.Set("Authorization", "Bearer valid-token")

	rec := httptest.NewRecorder()
	Auth(parserMock{principal: identity.Principal{UserID: 42}})(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotUserID != 42 {
		t.Fatalf("expected user id 42, got %d", gotUserID)
	}
}

func TestLoggingWritesOneLine(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("short and stout"))
	})

	rec := httptest.NewRecorder()
	Logging(log)(next).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/v1/teams", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("middleware must not change status, got %d", rec.Code)
	}
	line := buf.String()
	for _, want := range []string{`"method":"GET"`, `"path":"/api/v1/teams"`, `"status":418`} {
		if !strings.Contains(line, want) {
			t.Errorf("log line missing %s: %s", want, line)
		}
	}
}

func TestLoggingSkipsProbes(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, path := range []string{"/livez", "/readyz"} {
		rec := httptest.NewRecorder()
		Logging(log)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("probe %s must pass through, got %d", path, rec.Code)
		}
	}
	if buf.Len() != 0 {
		t.Fatalf("probes must not be logged, got: %s", buf.String())
	}
}

type limiterMock struct {
	allowed    bool
	retryAfter time.Duration
}

func (l limiterMock) Allow(ctx context.Context, key string) (bool, time.Duration) {
	return l.allowed, l.retryAfter
}

func TestRateLimitAllows(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{UserID: 42}))

	rec := httptest.NewRecorder()
	RateLimit(limiterMock{allowed: true})(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimitRejects(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when limit exceeded")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req = req.WithContext(identity.WithPrincipal(req.Context(), identity.Principal{UserID: 42}))

	rec := httptest.NewRecorder()
	RateLimit(limiterMock{allowed: false, retryAfter: 30 * time.Second})(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "31" {
		t.Fatalf("expected Retry-After 31, got %q", rec.Header().Get("Retry-After"))
	}
}

func TestRateLimitByIPRejects(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called when limit exceeded")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
	req.RemoteAddr = "10.0.0.7:51234"

	rec := httptest.NewRecorder()
	RateLimitByIP(limiterMock{allowed: false, retryAfter: 10 * time.Second}, nil)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}

func TestRateLimitByIPAllows(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
	req.RemoteAddr = "10.0.0.7:51234"

	rec := httptest.NewRecorder()
	RateLimitByIP(limiterMock{allowed: true}, nil)(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimitByIPTrustedBypass(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	_, trusted, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
	req.RemoteAddr = "10.0.0.7:51234"

	rec := httptest.NewRecorder()
	// the limiter would reject, but the trusted network skips it entirely
	RateLimitByIP(limiterMock{allowed: false}, []*net.IPNet{trusted})(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for trusted network, got %d", rec.Code)
	}
}

func TestRateLimitByIPUntrustedStillLimited(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next must not be called for untrusted ip over the limit")
	})

	_, trusted, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/login", nil)
	req.RemoteAddr = "203.0.113.5:44444"

	rec := httptest.NewRecorder()
	RateLimitByIP(limiterMock{allowed: false, retryAfter: 10 * time.Second}, []*net.IPNet{trusted})(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
}
