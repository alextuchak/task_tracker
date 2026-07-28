package integration

import (
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
	t.Parallel()
	owner := registerAndLogin(t, mail("t-create"))
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
	t.Parallel()
	owner := registerAndLogin(t, mail("t-out-owner"))
	outsider := registerAndLogin(t, mail("t-outsider"))
	teamID := createTeam(t, owner, "t-out")

	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", outsider,
		fmt.Sprintf(`{"team_id":%d,"title":"sneaky"}`, teamID))

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCreateTaskAssigneeMustBeMember(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-asg-owner"))
	strangerEmail := mail("t-asg-stranger")
	registerAndLogin(t, strangerEmail)
	teamID := createTeam(t, owner, "t-asg")

	strangerID := meID(t, login(t, strangerEmail, "password123"))
	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", owner,
		fmt.Sprintf(`{"team_id":%d,"title":"x","assignee_id":%d}`, teamID, strangerID))

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestListFiltersAndPagination(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-list"))
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
	t.Parallel()
	owner := registerAndLogin(t, mail("t-lnm-owner"))
	outsider := registerAndLogin(t, mail("t-lnm-out"))
	teamID := createTeam(t, owner, "t-lnm")

	resp := doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks?team_id=%d", teamID), outsider, "")

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestUpdateWritesHistoryAndCompletedAt(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-hist"))
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

// A task can be created already done. Nothing sets completed_at on that path,
// so the row is invisible to the "done in the last 7 days" report forever.
func TestCreateTaskAsDoneIsCountedByTheReport(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-cd-owner"))
	teamID := createTeam(t, owner, "t-cd")

	resp := doJSON(t, http.MethodPost, "/api/v1/tasks", owner,
		fmt.Sprintf(`{"team_id":%d,"title":"born done","status":"done"}`, teamID))
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var task taskDTO
	decodeJSON(t, readBody(t, resp), &task)
	require.NotNil(t, task.CompletedAt, "a task created as done must carry a completion time")

	admin := makeAdmin(t, mail("t-cd-admin"))
	for _, s := range fetchTeamStats(t, admin) {
		if s.ID == teamID {
			assert.Equal(t, int64(1), s.DoneLast7Days)
			return
		}
	}
	t.Fatal("team missing from the report")
}

func TestUpdateKeepsCompletedAtOnRepeatedDone(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-cc-owner"))
	teamID := createTeam(t, owner, "t-cc")
	task := createTask(t, owner, teamID, "task")

	require.NotNil(t, updateTask(t, owner, task.ID, `{"title":"task","status":"done"}`).CompletedAt)
	_, err := testDB.Exec(
		`UPDATE tasks SET completed_at = NOW() - INTERVAL 30 DAY WHERE id = ?`, task.ID)
	require.NoError(t, err)

	updateTask(t, owner, task.ID, `{"title":"renamed","status":"done"}`)

	admin := makeAdmin(t, mail("t-cc-admin"))
	for _, s := range fetchTeamStats(t, admin) {
		if s.ID == teamID {
			assert.Zero(t, s.DoneLast7Days,
				"editing a done task must not drag it back into the 7-day window")
			return
		}
	}
	t.Fatal("team missing from the report")
}

func TestUpdateClearsCompletedAtOnReopen(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-cr-owner"))
	teamID := createTeam(t, owner, "t-cr")
	task := createTask(t, owner, teamID, "task")
	require.NotNil(t, updateTask(t, owner, task.ID, `{"title":"task","status":"done"}`).CompletedAt)

	reopened := updateTask(t, owner, task.ID, `{"title":"task","status":"todo"}`)

	assert.Nil(t, reopened.CompletedAt, "a reopened task is not done")
}

func updateTask(t *testing.T, bearer string, id int64, body string) taskDTO {
	t.Helper()
	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", id), bearer, body)
	payload := readBody(t, resp)
	require.Equal(t, http.StatusOK, resp.StatusCode, payload)
	var task taskDTO
	decodeJSON(t, payload, &task)
	return task
}

func TestUpdateByOutsiderMasked(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-upd-owner"))
	outsider := registerAndLogin(t, mail("t-upd-out"))
	teamID := createTeam(t, owner, "t-upd")
	task := createTask(t, owner, teamID, "task")

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", task.ID), outsider,
		`{"title":"hacked","status":"todo"}`)

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHistoryByOutsiderMasked(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-ho-owner"))
	outsider := registerAndLogin(t, mail("t-ho-out"))
	teamID := createTeam(t, owner, "t-ho")
	task := createTask(t, owner, teamID, "task")

	resp := doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/history", task.ID), outsider, "")

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGlobalAdminUpdatesForeignTask(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("t-ga-owner"))
	teamID := createTeam(t, owner, "t-ga")
	task := createTask(t, owner, teamID, "task")

	admin := makeAdmin(t, mail("t-ga-admin"))
	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", task.ID), admin,
		`{"title":"task","status":"in_progress"}`)

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestUpdateUnknownTask(t *testing.T) {
	t.Parallel()
	bearer := registerAndLogin(t, mail("t-unk"))

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
