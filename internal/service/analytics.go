package service

import (
	"context"
	"fmt"
	"task_tracker/internal/domain"
)

type AnalyticsRepository interface {
	TeamStats(ctx context.Context, afterID int64, limit int) ([]domain.TeamStats, error)
	TopCreators(ctx context.Context, afterID int64, limit int) ([]domain.TeamTopCreator, error)
	OrphanAssignees(ctx context.Context, afterID int64, limit int) ([]domain.OrphanAssignee, error)
}

func NewAnalytics(repo AnalyticsRepository, authz *Authorizer) *Analytics {
	return &Analytics{repo: repo, authz: authz}
}

type Analytics struct {
	repo  AnalyticsRepository
	authz *Authorizer
}

func (s *Analytics) TeamStats(ctx context.Context, actorID, afterID int64, limit int) ([]domain.TeamStats, error) {
	if err := s.authz.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	stats, err := s.repo.TeamStats(ctx, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("team stats: %w", err)
	}
	return stats, nil
}

func (s *Analytics) TopCreators(ctx context.Context, actorID, afterID int64, limit int) ([]domain.TeamTopCreator, error) {
	if err := s.authz.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	creators, err := s.repo.TopCreators(ctx, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("top creators: %w", err)
	}
	return creators, nil
}

func (s *Analytics) OrphanAssignees(ctx context.Context, actorID, afterID int64, limit int) ([]domain.OrphanAssignee, error) {
	if err := s.authz.RequireAdmin(ctx, actorID); err != nil {
		return nil, err
	}
	orphans, err := s.repo.OrphanAssignees(ctx, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("orphan assignees: %w", err)
	}
	return orphans, nil
}
