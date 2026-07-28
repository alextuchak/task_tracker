package integration

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"task_tracker/internal/domain"
	"task_tracker/internal/infrastructure/cache"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskListServedFromCache(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("cache-hit"))
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
	t.Parallel()
	owner := registerAndLogin(t, mail("cache-inv"))
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
	t.Parallel()
	owner := registerAndLogin(t, mail("cache-crt"))
	teamID := createTeam(t, owner, "cache-crt-team")
	createTask(t, owner, teamID, "first")
	require.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items, 1)

	createTask(t, owner, teamID, "second")

	assert.Len(t, listTasks(t, owner, fmt.Sprintf("team_id=%d", teamID)).Items, 2)
}

// Invalidation is a post-commit effect: a client hanging up must not leave the
// list stale for the whole TTL.
func TestCacheInvalidationSurvivesACancelledCaller(t *testing.T) {
	t.Parallel()
	c := cache.NewTasks(testRedis, time.Minute, slog.New(slog.DiscardHandler))
	filter := domain.TaskFilter{TeamID: 1_000_000 + seq.Add(1), Limit: 20}
	c.SetList(context.Background(), filter, []domain.Task{{ID: 1, Title: "cached"}})
	_, ok := c.GetList(context.Background(), filter)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c.InvalidateTeam(ctx, filter.TeamID)

	_, ok = c.GetList(context.Background(), filter)
	assert.False(t, ok, "a cancelled caller must not leave the list stale")
}

// A team's write must not flush every other team's cache.
func TestCacheInvalidationIsScopedToItsTeam(t *testing.T) {
	t.Parallel()
	c := cache.NewTasks(testRedis, time.Minute, slog.New(slog.DiscardHandler))
	mine := domain.TaskFilter{TeamID: 2_000_000 + seq.Add(1), Limit: 20}
	theirs := domain.TaskFilter{TeamID: 2_000_000 + seq.Add(1), Limit: 20}
	c.SetList(context.Background(), mine, []domain.Task{{ID: 1}})
	c.SetList(context.Background(), theirs, []domain.Task{{ID: 2}})

	c.InvalidateTeam(context.Background(), mine.TeamID)

	_, ok := c.GetList(context.Background(), mine)
	assert.False(t, ok)
	_, ok = c.GetList(context.Background(), theirs)
	assert.True(t, ok, "one team's write must not flush the whole cache")
}
