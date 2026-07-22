package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func registerAndLogin(t *testing.T, email string) string {
	t.Helper()
	register(t, email, "User", "password123")
	return login(t, email, "password123")
}

func createTeam(t *testing.T, bearer, name string) int64 {
	t.Helper()
	resp := doJSON(t, http.MethodPost, "/api/v1/teams", bearer, fmt.Sprintf(`{"name":%q}`, name))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var got struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	return got.ID
}

func TestCreateTeamCreatorBecomesOwner(t *testing.T) {
	bearer := registerAndLogin(t, "owner1@teams.io")

	resp := doJSON(t, http.MethodPost, "/api/v1/teams", bearer, `{"name":"backend"}`)

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var got struct {
		Name string `json:"name"`
		Role string `json:"role"`
		ID   int64  `json:"id"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	assert.Positive(t, got.ID)
	assert.Equal(t, "backend", got.Name)
	assert.Equal(t, "owner", got.Role)
}

func TestCreateTeamValidation(t *testing.T) {
	bearer := registerAndLogin(t, "owner2@teams.io")

	resp := doJSON(t, http.MethodPost, "/api/v1/teams", bearer, `{"name":""}`)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListShowsOnlyOwnTeams(t *testing.T) {
	alice := registerAndLogin(t, "alice@teams.io")
	bob := registerAndLogin(t, "bob@teams.io")
	createTeam(t, alice, "alice-team")
	createTeam(t, bob, "bob-team")

	resp := doJSON(t, http.MethodGet, "/api/v1/teams", alice, "")

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got []struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	require.Len(t, got, 1)
	assert.Equal(t, "alice-team", got[0].Name)
	assert.Equal(t, "owner", got[0].Role)
}

func TestInviteByOwner(t *testing.T) {
	owner := registerAndLogin(t, "inv-owner@teams.io")
	registerAndLogin(t, "invitee@teams.io")
	teamID := createTeam(t, owner, "inv-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"invitee@teams.io"}`)

	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	invitee := login(t, "invitee@teams.io", "password123")
	listResp := doJSON(t, http.MethodGet, "/api/v1/teams", invitee, "")
	var teams []struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	decodeJSON(t, readBody(t, listResp), &teams)
	require.Len(t, teams, 1)
	assert.Equal(t, "inv-team", teams[0].Name)
	assert.Equal(t, "member", teams[0].Role)
}

func TestInviteByMemberForbidden(t *testing.T) {
	owner := registerAndLogin(t, "mf-owner@teams.io")
	registerAndLogin(t, "mf-member@teams.io")
	registerAndLogin(t, "mf-target@teams.io")
	teamID := createTeam(t, owner, "mf-team")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"mf-member@teams.io"}`)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	member := login(t, "mf-member@teams.io", "password123")
	resp = doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), member,
		`{"email":"mf-target@teams.io"}`)

	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestInviteByOutsiderMaskedAsNotFound(t *testing.T) {
	owner := registerAndLogin(t, "out-owner@teams.io")
	outsider := registerAndLogin(t, "outsider@teams.io")
	teamID := createTeam(t, owner, "out-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), outsider,
		`{"email":"out-owner@teams.io"}`)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestInviteByGlobalAdminIntoForeignTeam(t *testing.T) {
	owner := registerAndLogin(t, "ga-owner@teams.io")
	registerAndLogin(t, "ga-admin@teams.io")
	registerAndLogin(t, "ga-target@teams.io")
	teamID := createTeam(t, owner, "ga-team")
	require.NoError(t, authSvc.GrantAdmin(context.Background(), "ga-admin@teams.io"))

	admin := login(t, "ga-admin@teams.io", "password123")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), admin,
		`{"email":"ga-target@teams.io"}`)

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestInviteDuplicate(t *testing.T) {
	owner := registerAndLogin(t, "dup-owner@teams.io")
	registerAndLogin(t, "dup-invitee@teams.io")
	teamID := createTeam(t, owner, "dup-team")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"dup-invitee@teams.io"}`)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp = doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"dup-invitee@teams.io"}`)

	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestInviteUnknownEmail(t *testing.T) {
	owner := registerAndLogin(t, "ue-owner@teams.io")
	teamID := createTeam(t, owner, "ue-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"ghost@teams.io"}`)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestInviteSendsEmail(t *testing.T) {
	owner := registerAndLogin(t, "em-owner@teams.io")
	registerAndLogin(t, "em-invitee@teams.io")
	teamID := createTeam(t, owner, "em-team")
	before := emailMock.received.Load()

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"em-invitee@teams.io"}`)

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, before+1, emailMock.received.Load())
}

func TestInviteSucceedsWhenEmailServiceDown(t *testing.T) {
	owner := registerAndLogin(t, "down-owner@teams.io")
	registerAndLogin(t, "down-invitee@teams.io")
	teamID := createTeam(t, owner, "down-team")
	emailMock.failing.Store(true)
	t.Cleanup(func() { emailMock.failing.Store(false) })

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"down-invitee@teams.io"}`)

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}
