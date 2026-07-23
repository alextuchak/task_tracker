package service

import (
	"context"
	"fmt"
	"task_tracker/internal/domain"
	"task_tracker/internal/infrastructure/outbox"
)

type TeamRepository interface {
	Create(ctx context.Context, name string, creatorID int64) (int64, error)
	ListByUser(ctx context.Context, userID int64) ([]domain.TeamMembership, error)
	MemberRole(ctx context.Context, teamID, userID int64) (domain.TeamRole, error)
	AddMember(ctx context.Context, teamID, userID int64, role domain.TeamRole) error
}

type OutboxWriter interface {
	Enqueue(ctx context.Context, msg outbox.Message) error
}

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

func NewTeams(teams TeamRepository, users UserRepository, outbox OutboxWriter,
	tx TxManager, authz *Authorizer,
) *Teams {
	return &Teams{teams: teams, users: users, outbox: outbox, tx: tx, authz: authz}
}

type Teams struct {
	teams  TeamRepository
	users  UserRepository
	outbox OutboxWriter
	tx     TxManager
	authz  *Authorizer
}

func (s *Teams) Create(ctx context.Context, actorID int64, name string) (domain.TeamMembership, error) {
	var teamID int64
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var err error
		if teamID, err = s.teams.Create(ctx, name, actorID); err != nil {
			return err
		}
		return s.teams.AddMember(ctx, teamID, actorID, domain.TeamRoleOwner)
	})
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
	if err := s.authz.RequireTeamRole(ctx, actorID, teamID, domain.TeamRoleAdmin); err != nil {
		return err
	}
	invitee, err := s.users.ByEmail(ctx, inviteeEmail)
	if err != nil {
		return fmt.Errorf("find invitee: %w", err)
	}
	// membership and the invite email are committed together: the email is
	// queued in the outbox, never sent inline, and drained by the relay.
	return s.tx.Do(ctx, func(ctx context.Context) error {
		if err := s.teams.AddMember(ctx, teamID, invitee.ID, domain.TeamRoleMember); err != nil {
			return err
		}
		return s.outbox.Enqueue(ctx, outbox.Message{
			Recipient: inviteeEmail,
			Subject:   "You have been invited to a team",
			Body:      fmt.Sprintf("You have been added to team %d. Open Task Tracker to get started.", teamID),
		})
	})
}
