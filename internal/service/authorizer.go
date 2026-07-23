package service

import (
	"context"
	"errors"
	"fmt"
	"task_tracker/internal/domain"
)

func NewAuthorizer(users UserRepository, teams TeamRepository) *Authorizer {
	return &Authorizer{users: users, teams: teams}
}

type Authorizer struct {
	users UserRepository
	teams TeamRepository
}

func (a *Authorizer) RequireAdmin(ctx context.Context, actorID int64) error {
	actor, err := a.users.ByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("find actor: %w", err)
	}
	if actor.Role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	return nil
}

func (a *Authorizer) RequireTeamRole(ctx context.Context, actorID, teamID int64, min domain.TeamRole) error {
	actor, err := a.users.ByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("find actor: %w", err)
	}
	if actor.Role == domain.RoleAdmin {
		return nil
	}
	role, err := a.teams.MemberRole(ctx, teamID, actorID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("member role: %w", err)
	}
	if !role.AtLeast(min) {
		return domain.ErrForbidden
	}
	return nil
}
