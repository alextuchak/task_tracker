package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterCreated(t *testing.T) {
	resp := doJSON(t, http.MethodPost, "/api/v1/register", "",
		`{"email":"ada@test.io","name":"Ada","password":"password123"}`)

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	body := readBody(t, resp)
	var got struct {
		Email string `json:"email"`
		Name  string `json:"name"`
		ID    int64  `json:"id"`
	}
	decodeJSON(t, body, &got)
	assert.Positive(t, got.ID)
	assert.Equal(t, "ada@test.io", got.Email)
	assert.Equal(t, "Ada", got.Name)
	assert.NotContains(t, body, "password")
}

func TestRegisterDuplicateEmail(t *testing.T) {
	register(t, "twice@test.io", "First", "password123")

	resp := doJSON(t, http.MethodPost, "/api/v1/register", "",
		`{"email":"twice@test.io","name":"Second","password":"password123"}`)

	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestRegisterValidation(t *testing.T) {
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
	register(t, "login@test.io", "Ada", "password123")

	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		`{"email":"login@test.io","password":"password123"}`)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		AccessToken string `json:"access_token"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	require.NotEmpty(t, got.AccessToken)
	assert.Len(t, strings.Split(got.AccessToken, "."), 3)
}

func TestLoginWrongPassword(t *testing.T) {
	register(t, "wrongpass@test.io", "Ada", "password123")

	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		`{"email":"wrongpass@test.io","password":"not-the-password"}`)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestLoginUnknownEmail(t *testing.T) {
	resp := doJSON(t, http.MethodPost, "/api/v1/login", "",
		`{"email":"ghost@test.io","password":"password123"}`)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
