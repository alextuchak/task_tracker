package service

import (
	"context"
	"errors"
	"task_tracker/internal/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACommentIsNotWrittenWhenTheTaskCannotBeLoaded(t *testing.T) {
	tasks := &taskStub{byID: func(int64) (domain.Task, error) {
		return domain.Task{}, errors.New("driver: bad connection")
	}}
	comments := &commentStub{}
	s := NewComments(comments, tasks, &txStub{}, NewAuthorizer(&userStub{}, &teamStub{}))

	_, err := s.Create(context.Background(), 1, 9, "body")

	require.Error(t, err)
	assert.Empty(t, comments.seen)
}

func TestAnUnreadableTaskDoesNotAuthorizeAnEdit(t *testing.T) {
	comments := &commentStub{byID: func(int64) (domain.TaskComment, error) {
		return domain.TaskComment{ID: 5, TaskID: 9, UserID: 1}, nil
	}}
	tasks := &taskStub{byID: func(int64) (domain.Task, error) {
		return domain.Task{}, errors.New("driver: bad connection")
	}}
	s := NewComments(comments, tasks, &txStub{}, NewAuthorizer(&userStub{}, memberOf()))

	_, err := s.Update(context.Background(), 1, 5, "reworded")

	require.ErrorContains(t, err, "bad connection")
	assert.False(t, comments.has("comments.Update"))
}
