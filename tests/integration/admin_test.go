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
	t.Parallel()
	email := mail("plain")
	register(t, email, "Ada", "password123")
	bearer := login(t, email, "password123")

	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		Role string `json:"role"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	assert.Equal(t, "user", got.Role)
}

func TestGrantAdminVisibleViaAPI(t *testing.T) {
	t.Parallel()
	email := mail("root")
	register(t, email, "Root", "password123")

	require.NoError(t, authSvc.GrantAdmin(context.Background(), email))
	require.NoError(t, authSvc.GrantAdmin(context.Background(), email))

	bearer := login(t, email, "password123")
	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		Role string `json:"role"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	assert.Equal(t, "admin", got.Role)
}

func TestGrantAdminUnknownEmail(t *testing.T) {
	t.Parallel()
	err := authSvc.GrantAdmin(context.Background(), "nobody@test.io")

	require.ErrorIs(t, err, domain.ErrNotFound)
}
