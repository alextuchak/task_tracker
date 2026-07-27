package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func TestRegisterCreated(t *testing.T) {
	t.Parallel()
	email := mail("ada")
	resp := doJSON(t, http.MethodPost, "/api/v1/register", "",
		fmt.Sprintf(`{"email":%q,"name":"Ada","password":"password123"}`, email))

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := readBody(t, resp)
	var got struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		ID    int64  `json:"id"`
	}
	decodeJSON(t, body, &got)
	assert.Positive(t, got.ID)
	assert.Equal(t, email, got.Email)
	assert.Equal(t, "Ada", got.Name)
	assert.NotContains(t, body, "password")
}

func TestRegisterHashesWithTheConfiguredCost(t *testing.T) {
	t.Parallel()
	email := mail("cost")
	register(t, email, "Ada", "password123")

	var hash string
	require.NoError(t, testDB.QueryRow(
		`SELECT password_hash FROM users WHERE email = ?`, email).Scan(&hash))

	cost, err := bcrypt.Cost([]byte(hash))
	require.NoError(t, err)
	assert.Equal(t, bcrypt.MinCost, cost,
		"the harness injects the cheapest cost; a hardcoded one would ignore the config")
}

func TestRegisterDuplicateEmail(t *testing.T) {
	t.Parallel()
	email := mail("twice")
	register(t, email, "First", "password123")

	resp := doJSON(t, http.MethodPost, "/api/v1/register", "",
		fmt.Sprintf(`{"email":%q,"name":"Second","password":"password123"}`, email))

	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRegisterValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"bad email", `{"email":"not-an-email","name":"Ada","password":"password123"}`},
		{"empty name", `{"email":"a@test.io","name":"","password":"password123"}`},
		{"short password", `{"email":"a@test.io","name":"Ada","password":"short"}`},
		{"broken json", `{broken`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, http.MethodPost, "/api/v1/register", "", tc.body)

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})
	}
}

func TestLoginReturnsWorkingJWT(t *testing.T) {
	t.Parallel()
	email := mail("login")
	register(t, email, "Ada", "password123")

	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		fmt.Sprintf(`{"email":%q,"password":"password123"}`, email))

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	require.NotEmpty(t, got.AccessToken)
	assert.Len(t, strings.Split(got.AccessToken, "."), 3)
}

func TestLoginWrongPassword(t *testing.T) {
	t.Parallel()
	email := mail("wrongpass")
	register(t, email, "Ada", "password123")

	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		fmt.Sprintf(`{"email":%q,"password":"not-the-password"}`, email))

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestLoginUnknownEmail(t *testing.T) {
	t.Parallel()
	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		`{"email":"ghost@test.io","password":"password123"}`)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
