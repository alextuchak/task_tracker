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
	stats := fetchTeamStats(t, admin)

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
	creators := fetchTopCreators(t, admin)

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

	_, err := testDB.Exec(`DELETE FROM team_members WHERE team_id = ? AND user_id = ?`, teamID, memberID)
	require.NoError(t, err)

	orphansAfter := fetchOrphans(t, admin)
	assert.Contains(t, orphansAfter, task.ID)
}

type teamStatDTO struct {
	Name          string `json:"name"`
	ID            int64  `json:"id"`
	Members       int64  `json:"members"`
	DoneLast7Days int64  `json:"done_last_7d"`
}

func fetchTeamStats(t *testing.T, bearer string) []teamStatDTO {
	t.Helper()
	all := make([]teamStatDTO, 0)
	cursor := ""
	for {
		resp := doJSON(t, http.MethodGet, "/api/v1/analytics/teams?limit=100"+cursor, bearer, "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var page struct {
			NextCursor *int64        `json:"next_cursor"`
			Items      []teamStatDTO `json:"items"`
		}
		decodeJSON(t, readBody(t, resp), &page)
		all = append(all, page.Items...)
		if page.NextCursor == nil {
			return all
		}
		cursor = fmt.Sprintf("&cursor=%d", *page.NextCursor)
	}
}

type topCreatorDTO struct {
	UserName     string `json:"user_name"`
	TeamID       int64  `json:"team_id"`
	TasksCreated int64  `json:"tasks_created"`
	Rank         int64  `json:"rank"`
}

func fetchTopCreators(t *testing.T, bearer string) []topCreatorDTO {
	t.Helper()
	all := make([]topCreatorDTO, 0)
	cursor := ""
	for {
		resp := doJSON(t, http.MethodGet, "/api/v1/analytics/top-creators?limit=100"+cursor, bearer, "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var page struct {
			NextCursor *int64          `json:"next_cursor"`
			Items      []topCreatorDTO `json:"items"`
		}
		decodeJSON(t, readBody(t, resp), &page)
		all = append(all, page.Items...)
		if page.NextCursor == nil {
			return all
		}
		cursor = fmt.Sprintf("&cursor=%d", *page.NextCursor)
	}
}

func fetchOrphans(t *testing.T, bearer string) []int64 {
	t.Helper()
	ids := make([]int64, 0)
	cursor := ""
	for {
		resp := doJSON(t, http.MethodGet, "/api/v1/analytics/orphan-assignees?limit=100"+cursor, bearer, "")
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var page struct {
			NextCursor *int64 `json:"next_cursor"`
			Items      []struct {
				TaskID int64 `json:"task_id"`
			} `json:"items"`
		}
		decodeJSON(t, readBody(t, resp), &page)
		for _, o := range page.Items {
			ids = append(ids, o.TaskID)
		}
		if page.NextCursor == nil {
			return ids
		}
		cursor = fmt.Sprintf("&cursor=%d", *page.NextCursor)
	}
}

func TestAnalyticsTeamStatsPagination(t *testing.T) {
	admin := makeAdmin(t, "an-pg-admin@an.io")
	owner := registerAndLogin(t, "an-pg-owner@an.io")
	for i := 0; i < 3; i++ {
		createTeam(t, owner, fmt.Sprintf("an-pg-team-%d", i))
	}

	resp := doJSON(t, http.MethodGet, "/api/v1/analytics/teams?limit=2", admin, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var page1 struct {
		NextCursor *int64        `json:"next_cursor"`
		Items      []teamStatDTO `json:"items"`
	}
	decodeJSON(t, readBody(t, resp), &page1)
	require.Len(t, page1.Items, 2)
	require.NotNil(t, page1.NextCursor)
	assert.Equal(t, page1.Items[1].ID, *page1.NextCursor)

	resp = doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/analytics/teams?limit=2&cursor=%d", *page1.NextCursor), admin, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var page2 struct {
		NextCursor *int64        `json:"next_cursor"`
		Items      []teamStatDTO `json:"items"`
	}
	decodeJSON(t, readBody(t, resp), &page2)
	require.NotEmpty(t, page2.Items)
	assert.Greater(t, page2.Items[0].ID, page1.Items[1].ID)
}
