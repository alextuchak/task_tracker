package service

import (
	"context"
	"errors"
	"fmt"
	"task_tracker/internal/domain"
)

type CommentRepository interface {
	Create(ctx context.Context, c domain.TaskComment) (int64, error)
	ByID(ctx context.Context, id int64) (domain.TaskComment, error)
	List(ctx context.Context, f domain.CommentFilter) ([]domain.TaskComment, error)
	Update(ctx context.Context, id int64, body string) (domain.TaskComment, error)
	Delete(ctx context.Context, id int64) error
}

func NewComments(
	comments CommentRepository, tasks TaskRepository, tx TxManager, authz *Authorizer,
) *Comments {
	return &Comments{comments: comments, tasks: tasks, tx: tx, authz: authz}
}

type Comments struct {
	comments CommentRepository
	tasks    TaskRepository
	tx       TxManager
	authz    *Authorizer
}

func (s *Comments) Create(ctx context.Context, actorID, taskID int64, body string) (domain.TaskComment, error) {
	var created domain.TaskComment
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		task, err := s.tasks.ByID(ctx, taskID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("load task: %w", err)
		}
		if err := s.authz.RequireTeamRole(ctx, actorID, task.TeamID, domain.TeamRoleMember); err != nil {
			return err
		}
		id, err := s.comments.Create(ctx, domain.TaskComment{
			TaskID: taskID, UserID: actorID, Body: body,
		})
		if err != nil {
			return fmt.Errorf("create comment: %w", err)
		}
		if created, err = s.comments.ByID(ctx, id); err != nil {
			return fmt.Errorf("load created comment: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.TaskComment{}, err
	}
	return created, nil
}

func (s *Comments) List(ctx context.Context, actorID int64, f domain.CommentFilter) ([]domain.TaskComment, error) {
	task, err := s.tasks.ByID(ctx, f.TaskID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	if err := s.authz.RequireTeamRole(ctx, actorID, task.TeamID, domain.TeamRoleMember); err != nil {
		return nil, err
	}
	comments, err := s.comments.List(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	return comments, nil
}

func (s *Comments) Update(ctx context.Context, actorID, commentID int64, body string) (domain.TaskComment, error) {
	var updated domain.TaskComment
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.requireCanModify(ctx, actorID, commentID); err != nil {
			return err
		}
		var err error
		if updated, err = s.comments.Update(ctx, commentID, body); err != nil {
			return fmt.Errorf("update comment: %w", err)
		}
		return nil
	})
	if err != nil {
		return domain.TaskComment{}, err
	}
	return updated, nil
}

func (s *Comments) Delete(ctx context.Context, actorID, commentID int64) error {
	return s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.requireCanModify(ctx, actorID, commentID); err != nil {
			return err
		}
		if err := s.comments.Delete(ctx, commentID); err != nil {
			return fmt.Errorf("delete comment: %w", err)
		}
		return nil
	})
}

func (s *Comments) requireCanModify(ctx context.Context, actorID, commentID int64) error {
	comment, err := s.comments.ByID(ctx, commentID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("load comment: %w", err)
	}
	task, err := s.tasks.ByID(ctx, comment.TaskID)
	if err != nil {
		return fmt.Errorf("load task: %w", err)
	}
	if err := s.authz.RequireTeamRole(ctx, actorID, task.TeamID, domain.TeamRoleMember); err != nil {
		return err
	}
	if comment.UserID == actorID {
		return nil
	}
	return s.authz.RequireAdmin(ctx, actorID)
}
