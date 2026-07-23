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

func NewTasks(rdb *redis.Client, ttl time.Duration, log *slog.Logger) *Tasks {
	return &Tasks{rdb: rdb, ttl: ttl, log: log}
}

type Tasks struct {
	rdb *redis.Client
	log *slog.Logger
	ttl time.Duration
}

func (c *Tasks) GetList(ctx context.Context, f domain.TaskFilter) ([]domain.Task, bool) {
	raw, err := c.rdb.Get(ctx, listKey(f)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false
	}
	if err != nil {
		c.log.Warn("tasks cache get failed", slog.Any("error", err))
		return nil, false
	}
	var tasks []domain.Task
	if err := json.Unmarshal(raw, &tasks); err != nil {
		return nil, false
	}
	return tasks, true
}

func (c *Tasks) SetList(ctx context.Context, f domain.TaskFilter, tasks []domain.Task) {
	raw, err := json.Marshal(tasks)
	if err != nil {
		return
	}
	if err := c.rdb.Set(ctx, listKey(f), raw, c.ttl).Err(); err != nil {
		c.log.Warn("tasks cache set failed", slog.Any("error", err))
	}
}

func (c *Tasks) InvalidateTeam(ctx context.Context, teamID int64) {
	pattern := fmt.Sprintf("tasks:%d:*", teamID)
	iter := c.rdb.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		if err := c.rdb.Del(ctx, iter.Val()).Err(); err != nil {
			c.log.Warn("tasks cache del failed", slog.Any("error", err))
		}
	}
	if err := iter.Err(); err != nil {
		c.log.Warn("tasks cache invalidate failed",
			slog.Int64("team_id", teamID), slog.Any("error", err))
	}
}

func listKey(f domain.TaskFilter) string {
	status := ""
	if f.Status != nil {
		status = string(*f.Status)
	}
	assignee := int64(0)
	if f.AssigneeID != nil {
		assignee = *f.AssigneeID
	}
	return fmt.Sprintf("tasks:%d:%s:%d:%d:%d", f.TeamID, status, assignee, f.AfterID, f.Limit)
}
