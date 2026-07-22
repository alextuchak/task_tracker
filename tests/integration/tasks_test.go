package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type taskDTO struct {
	CompletedAt *string `json:"completed_at"`
	AssigneeID  *int64  `json:"assignee_id"`
	Title       string  `json:"title"`
	Status      string  `json:"status"`
	ID          int64   `json:"id"`
	TeamID      int64   `json:"team_id"`
}

func createTask(t *testing.T, bearer string, teamID int64, title string) taskDTO {
	t.Helper()
	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", bearer,
		fmt.Sprintf(`{"team_id":%d,"title":%q}`, teamID, title))
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var task taskDTO
	decodeJSON(t, readBody(t, resp), &task)
	return task
}

type taskPage struct {
	NextCursor *int64    `json:"next_cursor"`
	Items      []taskDTO `json:"items"`
}

func listTasks(t *testing.T, bearer, query string) taskPage {
	t.Helper()
	resp := doJSON(t, http.MethodGet, "/api/v1/tasks?"+query, bearer, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var page taskPage
	decodeJSON(t, readBody(t, resp), &page)
	return page
}

func TestCreateTaskByMember(t *testing.T) {
	owner := registerAndLogin(t, "t-create@tasks.io")
	teamID := createTeam(t, owner, "t-create")

	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", owner,
		fmt.Sprintf(`{"team_id":%d,"title":"first task","description":"details"}`, teamID))

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var task taskDTO
	decodeJSON(t, readBody(t, resp), &task)
	assert.Positive(t, task.ID)
	assert.Equal(t, "todo", task.Status)
	assert.Nil(t, task.CompletedAt)
}

func TestCreateTaskByOutsiderMasked(t *testing.T) {
	owner := registerAndLogin(t, "t-out-owner@tasks.io")
	outsider := registerAndLogin(t, "t-outsider@tasks.io")
	teamID := createTeam(t, owner, "t-out")

	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", outsider,
		fmt.Sprintf(`{"team_id":%d,"title":"sneaky"}`, teamID))

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCreateTaskAssigneeMustBeMember(t *testing.T) {
	owner := registerAndLogin(t, "t-asg-owner@tasks.io")
	registerAndLogin(t, "t-asg-stranger@tasks.io")
	teamID := createTeam(t, owner, "t-asg")

	strangerID := meID(t, login(t, "t-asg-stranger@tasks.io", "password123"))
	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", owner,
		fmt.Sprintf(`{"team_id":%d,"title":"x","assignee_id":%d}`, teamID, strangerID))

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListFiltersAndPagination(t *testing.T) {
	owner := registerAndLogin(t, "t-list@tasks.io")
	teamID := createTeam(t, owner, "t-list")
	ownerID := meID(t, owner)

	for i := 1; i <= 5; i++ {
		createTask(t, owner, teamID, fmt.Sprintf("task-%d", i))
	}
	first := listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items[0]
	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", first.ID), owner,
		fmt.Sprintf(`{"title":"task-1","status":"in_progress","assignee_id":%d}`, ownerID))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items, 5)
	assert.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d&status=in_progress", teamID)).Items, 1)
	assert.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d&assignee_id=%d", teamID, ownerID)).Items, 1)

	page1 := listTasks(t, owner, fmt.Sprintf("team_id=%d&limit=2", teamID))
	require.Len(t, page1.Items, 2)
	require.NotNil(t, page1.NextCursor)
	assert.Equal(t, page1.Items[1].ID, *page1.NextCursor)

	page2 := listTasks(t, owner, fmt.Sprintf("team_id=%d&limit=2&cursor=%d", teamID, *page1.NextCursor))
	require.Len(t, page2.Items, 2)
	require.NotNil(t, page2.NextCursor)
	assert.Greater(t, page2.Items[0].ID, page1.Items[1].ID)

	page3 := listTasks(t, owner, fmt.Sprintf("team_id=%d&limit=2&cursor=%d", teamID, *page2.NextCursor))
	require.Len(t, page3.Items, 1)
	assert.Equal(t, "task-5", page3.Items[0].Title)
	assert.Nil(t, page3.NextCursor)
}

func TestListByNonMemberMasked(t *testing.T) {
	owner := registerAndLogin(t, "t-lnm-owner@tasks.io")
	outsider := registerAndLogin(t, "t-lnm-out@tasks.io")
	teamID := createTeam(t, owner, "t-lnm")

	resp := doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks?team_id=%d", teamID), outsider, "")

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUpdateWritesHistoryAndCompletedAt(t *testing.T) {
	owner := registerAndLogin(t, "t-hist@tasks.io")
	teamID := createTeam(t, owner, "t-hist")
	task := createTask(t, owner, teamID, "old title")

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", task.ID), owner,
		`{"title":"new title","status":"done"}`)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var updated taskDTO
	decodeJSON(t, readBody(t, resp), &updated)
	assert.Equal(t, "new title", updated.Title)
	assert.Equal(t, "done", updated.Status)
	assert.NotNil(t, updated.CompletedAt)

	histResp := doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/history", task.ID), owner, "")
	require.Equal(t, http.StatusOK, histResp.StatusCode)
	var changes []struct {
		Field         string `json:"field"`
		OldValue      string `json:"old_value"`
		NewValue      string `json:"new_value"`
		ChangeGroupID string `json:"change_group_id"`
	}
	decodeJSON(t, readBody(t, histResp), &changes)
	require.Len(t, changes, 3)
	byField := map[string][2]string{}
	groups := map[string]string{}
	for _, c := range changes {
		byField[c.Field] = [2]string{c.OldValue, c.NewValue}
		groups[c.Field] = c.ChangeGroupID
	}
	assert.Contains(t, byField, "created")
	assert.Equal(t, [2]string{"old title", "new title"}, byField["title"])
	assert.Equal(t, [2]string{"todo", "done"}, byField["status"])
	assert.Equal(t, groups["title"], groups["status"], "поля одного PUT в одной группе")
	assert.NotEqual(t, groups["created"], groups["title"], "created — отдельная группа")
	assert.NotEmpty(t, groups["created"])
}

func TestUpdateByOutsiderMasked(t *testing.T) {
	owner := registerAndLogin(t, "t-upd-owner@tasks.io")
	outsider := registerAndLogin(t, "t-upd-out@tasks.io")
	teamID := createTeam(t, owner, "t-upd")
	task := createTask(t, owner, teamID, "task")

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", task.ID), outsider,
		`{"title":"hacked","status":"todo"}`)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHistoryByOutsiderMasked(t *testing.T) {
	owner := registerAndLogin(t, "t-ho-owner@tasks.io")
	outsider := registerAndLogin(t, "t-ho-out@tasks.io")
	teamID := createTeam(t, owner, "t-ho")
	task := createTask(t, owner, teamID, "task")

	resp := doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/history", task.ID), outsider, "")

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGlobalAdminUpdatesForeignTask(t *testing.T) {
	owner := registerAndLogin(t, "t-ga-owner@tasks.io")
	registerAndLogin(t, "t-ga-admin@tasks.io")
	teamID := createTeam(t, owner, "t-ga")
	task := createTask(t, owner, teamID, "task")
	require.NoError(t, authSvc.GrantAdmin(context.Background(), "t-ga-admin@tasks.io"))

	admin := login(t, "t-ga-admin@tasks.io", "password123")
	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", task.ID), admin,
		`{"title":"task","status":"in_progress"}`)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestUpdateUnknownTask(t *testing.T) {
	bearer := registerAndLogin(t, "t-unk@tasks.io")

	resp := doJSON(t, http.MethodPut, "/api/v1/tasks/999999", bearer,
		`{"title":"x","status":"todo"}`)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func meID(t *testing.T, bearer string) int64 {
	t.Helper()
	resp := doJSON(t, http.MethodGet, "/api/v1/me", bearer, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var got struct {
		ID int64 `json:"id"`
	}
	decodeJSON(t, readBody(t, resp), &got)
	return got.ID
}
