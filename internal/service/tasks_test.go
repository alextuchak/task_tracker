package service

import (
	"context"
	"errors"
	"task_tracker/internal/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAFailedListIsNotCachedAsAnEmptyOne(t *testing.T) {
	tasks := &taskStub{list: func(domain.TaskFilter) ([]domain.Task, error) {
		return nil, errors.New("driver: bad connection")
	}}
	c := &cacheStub{}
	teams := memberOf()
	s := NewTasks(tasks, teams, c, &txStub{}, NewAuthorizer(&userStub{}, teams))

	_, err := s.List(context.Background(), 1, domain.TaskFilter{TeamID: 7, Limit: 20})

	require.ErrorContains(t, err, "bad connection", "the read has to be the thing that failed")
	assert.False(t, c.has("cache.SetList"), "the cache must not learn anything from a failed read")
}

func TestARefusedActorWritesNothing(t *testing.T) {
	users := &userStub{byID: func(int64) (domain.User, error) {
		return domain.User{ID: 1, Role: domain.RoleUser}, nil
	}}
	teams := &teamStub{memberRole: func(int64, int64) (domain.TeamRole, error) {
		return "", domain.ErrNotFound
	}}
	tasks := &taskStub{}
	s := NewTasks(tasks, teams, &cacheStub{}, &txStub{}, NewAuthorizer(users, teams))

	_, err := s.Create(context.Background(), 1, 7, TaskInput{Title: "t"})

	require.ErrorIs(t, err, domain.ErrNotFound)
	assert.Empty(t, tasks.seen, "an outsider must not reach the repository at all")
}

func TestTheListIsInvalidatedOnlyAfterTheUpdateCommits(t *testing.T) {
	tx := &txStub{}
	c := &cacheStub{}
	var bumpedBeforeCommit bool
	c.onInvalidate = func() {
		if !tx.committed {
			bumpedBeforeCommit = true
		}
	}
	tasks := &taskStub{byID: func(id int64) (domain.Task, error) {
		return domain.Task{ID: id, TeamID: 7}, nil
	}}
	teams := memberOf()
	s := NewTasks(tasks, teams, c, tx, NewAuthorizer(&userStub{}, teams))

	_, err := s.Update(context.Background(), 1, 42, TaskInput{Title: "t", Status: domain.TaskStatusDone})

	require.NoError(t, err)
	assert.Equal(t, []int64{7}, c.bumped, "the team that owns the task is the one that goes stale")
	assert.False(t, bumpedBeforeCommit, "readers must not be sent back to an uncommitted row")
}
