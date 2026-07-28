package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"task_tracker/internal/domain"
	"time"

	"github.com/redis/go-redis/v9"
)

// versionUnknown marks a version we could not read, so the caller knows not to
// fill: writing under a guessed one is what this scheme exists to prevent.
const versionUnknown = -1

func NewTasks(rdb *redis.Client, ttl time.Duration, log *slog.Logger) *Tasks {
	return &Tasks{rdb: rdb, ttl: ttl, log: log}
}

type Tasks struct {
	rdb *redis.Client
	log *slog.Logger
	ttl time.Duration
}

func (c *Tasks) GetList(ctx context.Context, f domain.TaskFilter) ([]domain.Task, int64, bool) {
	version, err := c.version(ctx, f.TeamID)
	if err != nil {
		c.log.Warn("tasks cache version get failed", slog.Any("error", err))
		return nil, versionUnknown, false
	}
	raw, err := c.rdb.Get(ctx, listKey(f, version)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, version, false
	}
	if err != nil {
		c.log.Warn("tasks cache get failed", slog.Any("error", err))
		return nil, version, false
	}
	var tasks []domain.Task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		c.log.Error("tasks cache entry undecodable, falling back to the database",
			slog.Int64("team_id", f.TeamID), slog.Any("error", err))
		return nil, version, false
	}
	return tasks, version, true
}

func (c *Tasks) SetList(ctx context.Context, f domain.TaskFilter, version int64, tasks []domain.Task) {
	if version < 0 {
		return
	}
	raw, err := json.Marshal(tasks)
	if err != nil {
		c.log.Error("tasks cache entry unencodable, caching disabled for this key",
			slog.Int64("team_id", f.TeamID), slog.Any("error", err))
		return
	}
	if err := c.rdb.Set(ctx, listKey(f, version), raw, c.ttl).Err(); err != nil {
		c.log.Warn("tasks cache set failed", slog.Any("error", err))
	}
}

func (c *Tasks) InvalidateTeam(ctx context.Context, teamID int64) {
	ctx = context.WithoutCancel(ctx)
	if err := c.rdb.Incr(ctx, versionKey(teamID)).Err(); err != nil {
		c.log.Error("tasks cache invalidate failed, the list is stale until it expires",
			slog.Int64("team_id", teamID), slog.Any("error", err))
	}
}

func (c *Tasks) version(ctx context.Context, teamID int64) (int64, error) {
	version, err := c.rdb.Get(ctx, versionKey(teamID)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return version, err
}

func versionKey(teamID int64) string {
	return fmt.Sprintf("tasks:ver:%d", teamID)
}

func listKey(f domain.TaskFilter, version int64) string {
	status := ""
	if f.Status != nil {
		status = string(*f.Status)
	}
	assignee := int64(0)
	if f.AssigneeID != nil {
		assignee = *f.AssigneeID
	}
	return fmt.Sprintf("tasks:%d:%d:%s:%d:%d:%d", f.TeamID, version, status, assignee, f.AfterID, f.Limit)
}
