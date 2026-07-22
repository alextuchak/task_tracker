package integration

import (
	"context"
	"net/http"
	"task_tracker/internal/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterGetsUserRole(t *testing.T) {
	register(t, "plain@test.io", "Ada", "password123")
	bearer := login(t, "plain@test.io", "password123")

	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		Role string `json:"role"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	assert.Equal(t, "user", got.Role)
}

func TestGrantAdminVisibleViaAPI(t *testing.T) {
	register(t, "root@test.io", "Root", "password123")

	require.NoError(t, authSvc.GrantAdmin(context.Background(), "root@test.io"))
	require.NoError(t, authSvc.GrantAdmin(context.Background(), "root@test.io"))

	bearer := login(t, "root@test.io", "password123")
	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		Role string `json:"role"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	assert.Equal(t, "admin", got.Role)
}

func TestGrantAdminUnknownEmail(t *testing.T) {
	err := authSvc.GrantAdmin(context.Background(), "nobody@test.io")

	require.ErrorIs(t, err, domain.ErrNotFound)
}
