package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"task_tracker/internal/domain"
)

type TeamRepository interface {
	Create(ctx context.Context, name string, creatorID int64) (int64, error)
	ListByUser(ctx context.Context, userID int64) ([]domain.TeamMembership, error)
	MemberRole(ctx context.Context, teamID, userID int64) (domain.TeamRole, error)
	AddMember(ctx context.Context, teamID, userID int64, role domain.TeamRole) error
}

type EmailSender interface {
	SendInvite(ctx context.Context, to string, teamID int64) error
}

func NewTeams(teams TeamRepository, users UserRepository, email EmailSender, log *slog.Logger) *Teams {
	return &Teams{teams: teams, users: users, email: email, log: log}
}

type Teams struct {
	teams TeamRepository
	users UserRepository
	email EmailSender
	log   *slog.Logger
}

func (s *Teams) Create(ctx context.Context, actorID int64, name string) (domain.TeamMembership, error) {
	teamID, err := s.teams.Create(ctx, name, actorID)
	if err != nil {
		return domain.TeamMembership{}, fmt.Errorf("create team: %w", err)
	}
	return domain.TeamMembership{ID: teamID, Name: name, Role: domain.TeamRoleOwner}, nil
}

func (s *Teams) List(ctx context.Context, actorID int64) ([]domain.TeamMembership, error) {
	memberships, err := s.teams.ListByUser(ctx, actorID)
	if err != nil {
		return nil, fmt.Errorf("list teams: %w", err)
	}
	return memberships, nil
}

func (s *Teams) Invite(ctx context.Context, actorID, teamID int64, inviteeEmail string) error {
	if err := s.authorize(ctx, actorID, teamID, domain.TeamRoleAdmin); err != nil {
		return err
	}
	invitee, err := s.users.ByEmail(ctx, inviteeEmail)
	if err != nil {
		return fmt.Errorf("find invitee: %w", err)
	}
	if err := s.teams.AddMember(ctx, teamID, invitee.ID, domain.TeamRoleMember); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	if err := s.email.SendInvite(ctx, inviteeEmail, teamID); err != nil {
		s.log.Warn("invite email failed",
			slog.Int64("team_id", teamID), slog.Any("error", err))
	}
	return nil
}

func (s *Teams) authorize(ctx context.Context, actorID, teamID int64, min domain.TeamRole) error {
	actor, err := s.users.ByID(ctx, actorID)
	if err != nil {
		return fmt.Errorf("find actor: %w", err)
	}
	if actor.Role == domain.RoleAdmin {
		return nil
	}
	role, err := s.teams.MemberRole(ctx, teamID, actorID)
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
