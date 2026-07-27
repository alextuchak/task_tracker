package integration

import (
	"net/http"
	"strings"
	"task_tracker/internal/identity"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func login(t *testing.T, email, password string) string {
	t.Helper()
	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		`{"email":"`+email+`","password":"`+password+`"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	return got.AccessToken
}

func userID(t *testing.T, bearer string) int64 {
	t.Helper()
	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	return got.ID
}

func TestMeReturnsCurrentUser(t *testing.T) {
	t.Parallel()
	email := mail("me")
	register(t, email, "Ada", "password123")
	bearer := login(t, email, "password123")

	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		ID    int64  `json:"id"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	assert.Positive(t, got.ID)
	assert.Equal(t, email, got.Email)
	assert.Equal(t, "Ada", got.Name)
}

func TestMeWithoutToken(t *testing.T) {
	t.Parallel()
	resp := doJSON(t, http.MethodGet, "/api/v1/me", "", "")

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMeGarbageToken(t *testing.T) {
	t.Parallel()
	resp := doJSON(t, http.MethodGet, "/api/v1/me", "garbage", "")

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMeExpiredToken(t *testing.T) {
	t.Parallel()
	expiredIdp := identity.NewProvider(identity.Config{Secret: strings.Repeat("s", 32), TTL: -time.Minute})
	bearer, err := expiredIdp.Issue(1)
	require.NoError(t, err)

	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestMeForeignSignature(t *testing.T) {
	t.Parallel()
	foreignIdp := identity.NewProvider(identity.Config{Secret: strings.Repeat("x", 32), TTL: time.Hour})
	bearer, err := foreignIdp.Issue(1)
	require.NoError(t, err)

	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
