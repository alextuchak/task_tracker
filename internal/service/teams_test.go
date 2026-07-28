package service

import (
	"context"
	"errors"
	"task_tracker/internal/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnInviteThatCannotBeMailedIsNotAMembership(t *testing.T) {
	tx := &txStub{}
	out := &outboxStub{err: errors.New("outbox table is gone")}
	teams := &teamStub{memberRole: func(int64, int64) (domain.TeamRole, error) {
		return domain.TeamRoleOwner, nil
	}}
	s := NewTeams(teams, &userStub{}, out, tx, NewAuthorizer(&userStub{}, teams))

	err := s.Invite(context.Background(), 1, 7, "invitee@test.io")

	require.ErrorContains(t, err, "outbox table is gone")
	require.True(t, teams.has("teams.AddMember"), "the membership must have been attempted")
	assert.True(t, out.has("outbox.Enqueue"))
	assert.False(t, tx.committed, "the letter and the membership must go back together")
}
