package integration

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"task_tracker/internal/domain"
	"task_tracker/internal/infrastructure/cache"
	"task_tracker/internal/infrastructure/persistence"
	"task_tracker/internal/service"
	"testing"
	"time"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
	"github.com/avito-tech/go-transaction-manager/trm/v2/manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type writeBeforeFill struct {
	*cache.Tasks
	once  sync.Once
	reads atomic.Int32
	write func(ctx context.Context)
}

func (c *writeBeforeFill) GetList(ctx context.Context, f domain.TaskFilter) ([]domain.Task, int64, bool) {
	if c.reads.Add(1) > 1 {
		c.once.Do(func() { c.write(ctx) })
	}
	return c.Tasks.GetList(ctx, f)
}

func (c *writeBeforeFill) SetList(ctx context.Context, f domain.TaskFilter, version int64, tasks []domain.Task) {
	c.once.Do(func() { c.write(ctx) })
	c.Tasks.SetList(ctx, f, version, tasks)
}

func TestListDoesNotCacheAnAnswerAWriteHasAlreadyOverturned(t *testing.T) {
	t.Parallel()
	email := mail("cache-race")
	owner := registerAndLogin(t, email)
	teamID := createTeam(t, owner, "cache-race-team")
	createTask(t, owner, teamID, "first")
	ownerID := userIDByEmail(t, email)

	getter := trmsql.DefaultCtxGetter
	taskRepo := persistence.NewTaskRepo(testDB, getter)
	teamRepo := persistence.NewTeamRepo(testDB, getter)
	c := &writeBeforeFill{Tasks: cache.NewTasks(testRedis, time.Minute, slog.New(slog.DiscardHandler))}
	c.write = func(ctx context.Context) {
		_, err := taskRepo.Create(ctx, domain.Task{
			TeamID: teamID, Title: "second", Status: domain.TaskStatusTodo, CreatedBy: ownerID,
		})
		require.NoError(t, err)
		c.InvalidateTeam(ctx, teamID)
	}
	svc := service.NewTasks(taskRepo, teamRepo, c, manager.Must(trmsql.NewDefaultFactory(testDB)),
		service.NewAuthorizer(persistence.NewUserRepo(testDB, getter), teamRepo))

	filter := domain.TaskFilter{TeamID: teamID, Limit: 20}
	first, err := svc.List(context.Background(), ownerID, filter)
	require.NoError(t, err)
	require.Len(t, first, 1, "the write lands after this read, so it may not appear yet")

	second, err := svc.List(context.Background(), ownerID, filter)
	require.NoError(t, err)
	assert.Len(t, second, 2, "the fill lost to the write and must not be served")
}

func userIDByEmail(t *testing.T, email string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, testDB.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&id))
	return id
}
