package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeAdmin(t *testing.T, email string) string {
	t.Helper()
	registerAndLogin(t, email)
	require.NoError(t, authSvc.GrantAdmin(context.Background(), email))
	return login(t, email, "password123")
}

func TestAnalyticsForbiddenForRegularUser(t *testing.T) {
	user := registerAndLogin(t, "an-user@an.io")

	for _, path := range []string{
		"/api/v1/analytics/teams",
		"/api/v1/analytics/top-creators",
		"/api/v1/analytics/orphan-assignees",
	} {
		resp := doJSON(t, http.MethodGet, path, user, "")
		assert.Equal(t, http.StatusForbidden, resp.StatusCode, path)
	}
}

func TestAnalyticsTeamStats(t *testing.T) {
	owner := registerAndLogin(t, "an-ts-owner@an.io")
	registerAndLogin(t, "an-ts-member@an.io")
	teamID := createTeam(t, owner, "an-ts-team")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"an-ts-member@an.io"}`)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	fresh := createTask(t, owner, teamID, "fresh-done")
	resp = doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", fresh.ID), owner,
		`{"title":"fresh-done","status":"done"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	stale := createTask(t, owner, teamID, "stale-done")
	resp = doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", stale.ID), owner,
		`{"title":"stale-done","status":"done"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	_, err := testDB.Exec(`UPDATE tasks SET completed_at = NOW() - INTERVAL 10 DAY WHERE id = ?`, stale.ID)
	require.NoError(t, err)

	createTask(t, owner, teamID, "still-todo")

	admin := makeAdmin(t, "an-ts-admin@an.io")
	statsResp := doJSON(t, http.MethodGet, "/api/v1/analytics/teams", admin, "")
	require.Equal(t, http.StatusOK, statsResp.StatusCode)
	var stats []struct {
		Name          string `json:"name"`
		ID            int64  `json:"id"`
		Members       int64  `json:"members"`
		DoneLast7Days int64  `json:"done_last_7d"`
	}
	decodeJSON(t, readBody(t, statsResp), &stats)

	var found bool
	for _, s := range stats {
		if s.ID == teamID {
			found = true
			assert.Equal(t, "an-ts-team", s.Name)
			assert.Equal(t, int64(2), s.Members)
			assert.Equal(t, int64(1), s.DoneLast7Days, "10-дневный done не должен считаться")
		}
	}
	require.True(t, found, "team %d must be in stats", teamID)
}

func TestAnalyticsTopCreators(t *testing.T) {
	owner := registerAndLogin(t, "an-tc-owner@an.io")
	teamID := createTeam(t, owner, "an-tc-team")
	for _, email := range []string{"an-tc-u2@an.io", "an-tc-u3@an.io", "an-tc-u4@an.io"} {
		registerAndLogin(t, email)
		resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
			fmt.Sprintf(`{"email":%q}`, email))
		require.Equal(t, http.StatusNoContent, resp.StatusCode)
	}

	u2 := login(t, "an-tc-u2@an.io", "password123")
	u3 := login(t, "an-tc-u3@an.io", "password123")
	for i := 0; i < 3; i++ {
		createTask(t, owner, teamID, fmt.Sprintf("owner-%d", i))
	}
	for i := 0; i < 2; i++ {
		createTask(t, u2, teamID, fmt.Sprintf("u2-%d", i))
	}
	createTask(t, u3, teamID, "u3-0")

	admin := makeAdmin(t, "an-tc-admin@an.io")
	resp := doJSON(t, http.MethodGet, "/api/v1/analytics/top-creators", admin, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var creators []struct {
		UserName     string `json:"user_name"`
		TeamID       int64  `json:"team_id"`
		TasksCreated int64  `json:"tasks_created"`
		Rank         int64  `json:"rank"`
	}
	decodeJSON(t, readBody(t, resp), &creators)

	var team []struct {
		UserName     string
		TasksCreated int64
		Rank         int64
	}
	for _, c := range creators {
		if c.TeamID == teamID {
			team = append(team, struct {
				UserName     string
				TasksCreated int64
				Rank         int64
			}{c.UserName, c.TasksCreated, c.Rank})
		}
	}
	require.Len(t, team, 3, "ровно топ-3, четвёртый юзер без задач не попадает")
	assert.Equal(t, int64(1), team[0].Rank)
	assert.Equal(t, int64(3), team[0].TasksCreated)
	assert.Equal(t, int64(2), team[1].TasksCreated)
	assert.Equal(t, int64(1), team[2].TasksCreated)
}

func TestAnalyticsOrphanAssignees(t *testing.T) {
	owner := registerAndLogin(t, "an-or-owner@an.io")
	registerAndLogin(t, "an-or-member@an.io")
	teamID := createTeam(t, owner, "an-or-team")
	resp := doJSON(t, http.MethodPost, fmt.Sprintf("/api/v1/teams/%d/invite", teamID), owner,
		`{"email":"an-or-member@an.io"}`)
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	memberID := meID(t, login(t, "an-or-member@an.io", "password123"))
	resp = doJSON(t, http.MethodPost, "/api/v1/tasks", owner,
		fmt.Sprintf(`{"team_id":%d,"title":"assigned","assignee_id":%d}`, teamID, memberID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var task taskDTO
	decodeJSON(t, readBody(t, resp), &task)

	admin := makeAdmin(t, "an-or-admin@an.io")
	orphansBefore := fetchOrphans(t, admin)
	assert.NotContains(t, orphansBefore, task.ID, "пока member в команде — не сирота")

	// рассинхрон руками: приложение такого не создаёт, запрос должен ловить
	_, err := testDB.Exec(`DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, memberID)
	require.NoError(t, err)

	orphansAfter := fetchOrphans(t, admin)
	assert.Contains(t, orphansAfter, task.ID)
}

func fetchOrphans(t *testing.T, bearer string) []int64 {
	t.Helper()
	resp := doJSON(t, http.MethodGet, "/api/v1/analytics/orphan-assignees", bearer, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var orphans []struct {
		TaskID int64 `json:"task_id"`
	}
	decodeJSON(t, readBody(t, resp), &orphans)
	ids := make([]int64, 0, len(orphans))
	for _, o := range orphans {
		ids = append(ids, o.TaskID)
	}
	return ids
}
