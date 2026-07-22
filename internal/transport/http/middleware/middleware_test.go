package middleware

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"task_tracker/internal/identity"
	"testing"
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
