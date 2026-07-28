package service

import (
	"context"
	"errors"
	"task_tracker/internal/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnUnreadableMembershipIsNotAPolicyDecision(t *testing.T) {
	teams := &teamStub{memberRole: func(int64, int64) (domain.TeamRole, error) {
		return "", errors.New("driver: bad connection")
	}}
	a := NewAuthorizer(&userStub{}, teams)

	err := a.RequireTeamRole(context.Background(), 1, 7, domain.TeamRoleMember)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrForbidden)
	assert.NotErrorIs(t, err, domain.ErrNotFound)
}

func TestAnUnreadableActorIsNotADeniedActor(t *testing.T) {
	users := &userStub{byID: func(int64) (domain.User, error) {
		return domain.User{}, errors.New("driver: bad connection")
	}}
	a := NewAuthorizer(users, &teamStub{})

	err := a.RequireAdmin(context.Background(), 1)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrForbidden)
}

func TestAnAdminNeedsNoMembership(t *testing.T) {
	users := &userStub{byID: func(int64) (domain.User, error) {
		return domain.User{ID: 1, Role: domain.RoleAdmin}, nil
	}}
	teams := &teamStub{memberRole: func(int64, int64) (domain.TeamRole, error) {
		return "", domain.ErrNotFound
	}}
	a := NewAuthorizer(users, teams)

	require.NoError(t, a.RequireTeamRole(context.Background(), 1, 7, domain.TeamRoleOwner))
	assert.Empty(t, teams.seen, "an admin must not depend on being a member")
}

func TestAnUnreadableActorIsNotADeniedMember(t *testing.T) {
	users := &userStub{byID: func(int64) (domain.User, error) {
		return domain.User{}, errors.New("driver: bad connection")
	}}
	teams := memberOf()
	a := NewAuthorizer(users, teams)

	err := a.RequireTeamRole(context.Background(), 1, 7, domain.TeamRoleMember)

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrForbidden)
	assert.Empty(t, teams.seen, "an unidentified actor must not even be looked up")
}
