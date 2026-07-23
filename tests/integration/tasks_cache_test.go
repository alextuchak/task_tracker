package integration

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskListServedFromCache(t *testing.T) {
	owner := registerAndLogin(t, "cache-hit@cache.io")
	teamID := createTeam(t, owner, "cache-hit-team")
	task := createTask(t, owner, teamID, "cached title")

	first := listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID))
	require.Len(t, first.Items, 1)

	_, err := testDB.Exec(`UPDATE tasks SET title = 'changed behind cache' WHERE id = ?`, task.ID)
	require.NoError(t, err)

	second := listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID))
	require.Len(t, second.Items, 1)
	assert.Equal(t, "cached title", second.Items[0].Title, "список должен прийти из кеша")
}

func TestTaskListCacheInvalidatedOnUpdate(t *testing.T) {
	owner := registerAndLogin(t, "cache-inv@cache.io")
	teamID := createTeam(t, owner, "cache-inv-team")
	task := createTask(t, owner, teamID, "before")
	require.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items, 1)

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/tasks/%d", task.ID), owner,
		`{"title":"after","status":"in_progress"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	fresh := listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID))
	require.Len(t, fresh.Items, 1)
	assert.Equal(t, "after", fresh.Items[0].Title)
	assert.Equal(t, "in_progress", fresh.Items[0].Status)
}

func TestTaskListCacheInvalidatedOnCreate(t *testing.T) {
	owner := registerAndLogin(t, "cache-crt@cache.io")
	teamID := createTeam(t, owner, "cache-crt-team")
	createTask(t, owner, teamID, "first")
	require.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items, 1)

	createTask(t, owner, teamID, "second")

	assert.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items, 2)
}
