package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitKicksInPerUser(t *testing.T) {
	hammer := registerAndLogin(t, "hammer@rate.io")
	calm := registerAndLogin(t, "calm@rate.io")

	var limited *http.Response
	for i := 0; i < 160; i++ {
		resp := doJSON(t, http.MethodGet, "/api/v1/me", hammer, "")
		if resp.StatusCode == http.StatusTooManyRequests {
			limited = resp
			break
		}
		require.Equal(t, http.StatusOK, resp.StatusCode)
	}

	require.NotNil(t, limited, "160 requests must exceed the 150/min test limit")
	assert.NotEmpty(t, limited.Header.Get("Retry-After"))

	resp := doJSON(t, http.MethodGet, "/api/v1/me", calm, "")
	assert.Equal(t, http.StatusOK, resp.StatusCode, "another user must not be limited")
}
