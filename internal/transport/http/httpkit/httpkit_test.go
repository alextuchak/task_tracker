package httpkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInternalErrorKeepsTheCauseOutOfTheResponse(t *testing.T) {
	leaky := fmt.Errorf("load task: %w",
		errors.New(`sql: dial tcp 10.0.0.5:3306: bcrypt hash "$2a$10$abc"`))
	rec := httptest.NewRecorder()

	WriteInternalError(rec, httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil), leaky)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":"internal error"}`, rec.Body.String())
	for _, secret := range []string{"sql", "3306", "10.0.0.5", "bcrypt", "$2a$10$"} {
		assert.NotContains(t, rec.Body.String(), secret)
	}
}

// A client that hangs up is not a server fault: counting it as one poisons the
// 5xx rate that on-call alerts on.
func TestAbandonedRequestIsNotAServerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	WriteInternalError(rec, req, fmt.Errorf("list tasks: %w", context.Canceled))

	assert.Equal(t, 499, rec.Code, "nginx's 499 keeps an abandoned request out of the 5xx rate")
	assert.JSONEq(t, `{"error":"client closed request"}`, rec.Body.String())
}

// A disconnect must not launder a real backend failure out of the error rate.
func TestARealFailureOnAnAbandonedRequestIsStillLogged(t *testing.T) {
	var logs bytes.Buffer
	restore := captureDefaultLogger(t, &logs)
	defer restore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil).WithContext(ctx)

	WriteInternalError(httptest.NewRecorder(), req, errors.New("driver: bad connection"))

	assert.Contains(t, logs.String(), "driver: bad connection",
		"the cause must stay visible even when the client is gone")
	assert.Contains(t, logs.String(), `"level":"ERROR"`)
}

// The status is the only place an unhandled failure can still be diagnosed:
// the response deliberately says nothing.
func TestInternalErrorLogsTheCause(t *testing.T) {
	var logs bytes.Buffer
	restore := captureDefaultLogger(t, &logs)
	defer restore()

	WriteInternalError(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/v1/tasks", nil),
		errors.New("load task: driver: bad connection"))

	assert.Contains(t, logs.String(), "load task: driver: bad connection")
	assert.Contains(t, logs.String(), `"path":"/api/v1/tasks"`)
	assert.Contains(t, logs.String(), `"status":500`)
}

func TestEncodeFailureIsNotServedAsSuccess(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteJSON(rec, http.StatusOK, map[string]any{"bad": make(chan int)})

	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"a body that cannot be encoded is not a 200")
}

func captureDefaultLogger(t *testing.T, buf *bytes.Buffer) func() {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	return func() { slog.SetDefault(previous) }
}
