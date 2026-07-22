package service

import (
	"context"
	"errors"
	"fmt"
	"task_tracker/internal/domain"
)

type TaskRepository interface {
	Create(ctx context.Context, t domain.Task) (int64, error)
	ByID(ctx context.Context, id int64) (domain.Task, error)
	List(ctx context.Context, f domain.TaskFilter) ([]domain.Task, error)
	Update(ctx context.Context, actorID int64, t domain.Task) (domain.Task, error)
	History(ctx context.Context, taskID int64) ([]domain.TaskChange, error)
}

func NewTasks(tasks TaskRepository, teams TeamRepository, authz *Authorizer) *Tasks {
	return &Tasks{tasks: tasks, teams: teams, authz: authz}
}

type Tasks struct {
	tasks TaskRepository
	teams TeamRepository
	authz *Authorizer
}

type TaskInput struct {
	AssigneeID  *int64
	Title       string
	Description string
	Status      domain.TaskStatus
}

func (s *Tasks) Create(ctx context.Context, actorID, teamID int64, in TaskInput) (domain.Task, error) {
	if err := s.authz.RequireTeamRole(ctx, actorID, teamID, domain.TeamRoleMember); err != nil {
		return domain.Task{}, err
	}
	if err := s.requireAssigneeIsMember(ctx, teamID, in.AssigneeID); err != nil {
		return domain.Task{}, err
	}
	status := in.Status
	if status == "" {
		status = domain.TaskStatusTodo
	}
	id, err := s.tasks.Create(ctx, domain.Task{
		TeamID:      teamID,
		Title:       in.Title,
		Description: in.Description,
		Status:      status,
		AssigneeID:  in.AssigneeID,
		CreatedBy:   actorID,
	})
	if err != nil {
		return domain.Task{}, fmt.Errorf("create task: %w", err)
	}
	created, err := s.tasks.ByID(ctx, id)
	if err != nil {
		return domain.Task{}, fmt.Errorf("load created task: %w", err)
	}
	return created, nil
}

func (s *Tasks) List(ctx context.Context, actorID int64, f domain.TaskFilter) ([]domain.Task, error) {
	if err := s.authz.RequireTeamRole(ctx, actorID, f.TeamID, domain.TeamRoleMember); err != nil {
		return nil, err
	}
	tasks, err := s.tasks.List(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, nil
}

func (s *Tasks) Update(ctx context.Context, actorID, taskID int64, in TaskInput) (domain.Task, error) {
	current, err := s.tasks.ByID(ctx, taskID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Task{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("load task: %w", err)
	}
	if err := s.authz.RequireTeamRole(ctx, actorID, current.TeamID, domain.TeamRoleMember); err != nil {
		return domain.Task{}, err
	}
	if err := s.requireAssigneeIsMember(ctx, current.TeamID, in.AssigneeID); err != nil {
		return domain.Task{}, err
	}
	current.Title = in.Title
	current.Description = in.Description
	current.Status = in.Status
	current.AssigneeID = in.AssigneeID
	updated, err := s.tasks.Update(ctx, actorID, current)
	if err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}
	return updated, nil
}

func (s *Tasks) History(ctx context.Context, actorID, taskID int64) ([]domain.TaskChange, error) {
	task, err := s.tasks.ByID(ctx, taskID)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load task: %w", err)
	}
	if err := s.authz.RequireTeamRole(ctx, actorID, task.TeamID, domain.TeamRoleMember); err != nil {
		return nil, err
	}
	changes, err := s.tasks.History(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("task history: %w", err)
	}
	return changes, nil
}

func (s *Tasks) requireAssigneeIsMember(ctx context.Context, teamID int64, assigneeID *int64) error {
	if assigneeID == nil {
		return nil
	}
	_, err := s.teams.MemberRole(ctx, teamID, *assigneeID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotTeamMember
	}
	if err != nil {
		return fmt.Errorf("assignee role: %w", err)
	}
	return nil
}
