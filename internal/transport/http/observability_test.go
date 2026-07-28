package http

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"task_tracker/internal/infrastructure/health"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPanicIsRecordedAsAServedError(t *testing.T) {
	var logs bytes.Buffer
	r := chi.NewRouter()
	useObservability(r, slog.New(slog.NewJSONHandler(&logs, nil)))
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("handler exploded") })
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/boom")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Contains(t, logs.String(), `"status":500`,
		"the panic must be logged as the 500 the client received")
	assert.Contains(t, logs.String(), "handler panicked",
		"the cause must reach our own stream, not chi's stderr")

	metrics := httptest.NewRecorder()
	promhttp.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, metrics.Body.String(), `http_requests_total{method="GET",route="/boom",status="500"}`)
	assert.Contains(t, metrics.Body.String(), `http_request_errors_total{method="GET",route="/boom",status="500"}`,
		"a panic must count towards the error rate operators alert on")
}

// useObservability is only worth testing if NewRouter still uses it.
func TestTheRealRouterRecordsRequests(t *testing.T) {
	var logs bytes.Buffer
	r := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)),
		health.New(health.Config{CheckTimeout: time.Second}),
		nil, nil, nil, nil, nil, nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, logs.String(), `"path":"/nope"`,
		"NewRouter must keep the observability middlewares wired")
}

func TestReadyzKeepsTheDriverErrorInsideAndLogsIt(t *testing.T) {
	h := health.New(health.Config{CheckTimeout: time.Second})
	h.AddCheck(func(context.Context) error {
		return errors.New("dial tcp 10.0.0.5:3306: connect: connection refused")
	})
	h.SetReady()
	var logs bytes.Buffer

	rec := httptest.NewRecorder()
	readyzHandler(h, slog.New(slog.NewJSONHandler(&logs, nil)))(
		rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.NotContains(t, rec.Body.String(), "3306",
		"readyz is unauthenticated and must not describe our infrastructure")
	assert.Contains(t, rec.Body.String(), "not ready")
	assert.Contains(t, logs.String(), "3306",
		"the probe skips observability, so this log is the only record of why")
}
