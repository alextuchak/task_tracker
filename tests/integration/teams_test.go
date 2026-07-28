package integration

import (
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

func invite(t *testing.T, owner string, teamID int64, email string) {
	t.Helper()
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, email))
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
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
	t.Parallel()
	bearer := registerAndLogin(t, mail("owner1"))

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

func TestCreatingATeamAfterTheAccountIsGone(t *testing.T) {
	t.Parallel()
	email := mail("team-gone")
	register(t, email, "Ada", "password123")
	bearer := login(t, email, "password123")
	_, err := testDB.Exec(`DELETE FROM users WHERE email = ?`, email)
	require.NoError(t, err)

	resp := doJSON(t, http.MethodPost, "/api/v1/teams", bearer, `{"name":"backend"}`)

	require.Equal(t, http.StatusUnauthorized, resp.StatusCode, readBody(t, resp))
}

func TestCreateTeamValidation(t *testing.T) {
	t.Parallel()
	bearer := registerAndLogin(t, mail("owner2"))

	resp := doJSON(t, http.MethodPost, "/api/v1/teams", bearer, `{"name":""}`)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListShowsOnlyOwnTeams(t *testing.T) {
	t.Parallel()
	alice := registerAndLogin(t, mail("alice"))
	bob := registerAndLogin(t, mail("bob"))
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
	t.Parallel()
	owner := registerAndLogin(t, mail("inv-owner"))
	inviteeEmail := mail("invitee")
	registerAndLogin(t, inviteeEmail)
	teamID := createTeam(t, owner, "inv-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, inviteeEmail))

	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	invitee := login(t, inviteeEmail, "password123")
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
	t.Parallel()
	owner := registerAndLogin(t, mail("mf-owner"))
	memberEmail := mail("mf-member")
	registerAndLogin(t, memberEmail)
	targetEmail := mail("mf-target")
	registerAndLogin(t, targetEmail)
	teamID := createTeam(t, owner, "mf-team")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, memberEmail))
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	member := login(t, memberEmail, "password123")
	resp = doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), member,
		fmt.Sprintf(`{"email":%q}`, targetEmail))

	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestInviteByOutsiderMaskedAsNotFound(t *testing.T) {
	t.Parallel()
	ownerEmail := mail("out-owner")
	owner := registerAndLogin(t, ownerEmail)
	outsider := registerAndLogin(t, mail("outsider"))
	teamID := createTeam(t, owner, "out-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), outsider,
		fmt.Sprintf(`{"email":%q}`, ownerEmail))

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestInviteByGlobalAdminIntoForeignTeam(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("ga-owner"))

	targetEmail := mail("ga-target")
	registerAndLogin(t, targetEmail)
	teamID := createTeam(t, owner, "ga-team")
	admin := makeAdmin(t, mail("ga-admin"))
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), admin,
		fmt.Sprintf(`{"email":%q}`, targetEmail))

	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestInviteDuplicate(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("dup-owner"))
	inviteeEmail := mail("dup-invitee")
	registerAndLogin(t, inviteeEmail)
	teamID := createTeam(t, owner, "dup-team")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, inviteeEmail))
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp = doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, inviteeEmail))

	require.Equal(t, http.StatusConflict, resp.StatusCode)
}

func TestInviteUnknownEmail(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("ue-owner"))
	teamID := createTeam(t, owner, "ue-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"ghost@teams.io"}`)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestInviteEnqueuesEmail(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("em-owner"))
	inviteeEmail := mail("em-invitee")
	registerAndLogin(t, inviteeEmail)
	teamID := createTeam(t, owner, "em-team")

	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, inviteeEmail))

	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	var count int
	err := testDB.QueryRow(
		`SELECT COUNT(*) FROM email_outbox WHERE recipient = ? AND status = 'pending'`,
		inviteeEmail).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestInviteEnqueueIsAtomicWithMembership(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("atomic-owner"))
	inviteeEmail := mail("atomic-invitee")
	registerAndLogin(t, inviteeEmail)
	teamID := createTeam(t, owner, "atomic-team")

	// invite the same member twice: the second AddMember hits a duplicate and
	// rolls the whole transaction back, so no second outbox row is written.
	first := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, inviteeEmail))
	require.Equal(t, http.StatusNoContent, first.StatusCode)
	second := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		fmt.Sprintf(`{"email":%q}`, inviteeEmail))
	require.Equal(t, http.StatusConflict, second.StatusCode)

	var count int
	err := testDB.QueryRow(
		`SELECT COUNT(*) FROM email_outbox WHERE recipient = ?`,
		inviteeEmail).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
