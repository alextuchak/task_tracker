package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitKicksInPerUser(t *testing.T) {
	t.Parallel()
	hammer := registerAndLogin(t, mail("hammer"))
	calm := registerAndLogin(t, mail("calm"))

	var limited *http.Response
	start := time.Now()
	for i := 0; i < 400; i++ {
		resp := doJSON(t, http.MethodGet, "/api/v1/me", hammer, "")
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = resp
			break
		}
		require.Equal(t, http.StatusOK, resp.StatusCode)
		readBody(t, resp)
	}

	require.NotNil(t, limited,
		"400 requests in %s must exceed the 150/min test limit", time.Since(start))
	assert.NotEmpty(t, limited.Header.Get("Retry-After"))

	resp := doJSON(t, http.MethodGet, "/api/v1/me", calm, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "another user must not be limited")
}
