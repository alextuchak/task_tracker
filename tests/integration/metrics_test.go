package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsExposed(t *testing.T) {
	register(t, "metrics@obs.io", "Ada", "password123")

	resp := doJSON(t, http.MethodGet, "/metrics", "", "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body := readBody(t, resp)
	assert.Contains(t, body, `http_requests_total{method="POST",route="/api/v1/register",status="201"}`)
	assert.Contains(t, body, "http_request_duration_seconds_bucket")
	assert.Contains(t, body, "go_goroutines")
	assert.NotContains(t, body, `route="/metrics"`)
}
