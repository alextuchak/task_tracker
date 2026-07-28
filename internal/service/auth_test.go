package service

import (
	"context"
	"errors"
	"task_tracker/internal/domain"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestADatabaseFailureIsNotAWrongPassword(t *testing.T) {
	users := &userStub{byEmail: func(string) (domain.User, error) {
		return domain.User{}, errors.New("driver: bad connection")
	}}
	a := NewAuth(users, &tokenStub{}, bcryptTestCost)

	_, err := a.Login(context.Background(), "someone@test.io", "secret")

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestAnUnknownAddressLooksLikeAWrongPassword(t *testing.T) {
	users := &userStub{byEmail: func(string) (domain.User, error) {
		return domain.User{}, domain.ErrNotFound
	}}
	tokens := &tokenStub{}
	a := NewAuth(users, tokens, bcryptTestCost)

	_, err := a.Login(context.Background(), "nobody@test.io", "secret")

	assert.ErrorIs(t, err, domain.ErrInvalidCredentials)
	assert.Empty(t, tokens.seen, "no token may be minted for an address we do not know")
}

func TestAnUnsignableTokenIsNotARejectedLogin(t *testing.T) {
	hash := hashFor(t, "secret")
	users := &userStub{byEmail: func(string) (domain.User, error) {
		return domain.User{ID: 1, PasswordHash: hash}, nil
	}}
	tokens := &tokenStub{err: errors.New("no signing key")}
	a := NewAuth(users, tokens, bcryptTestCost)

	_, err := a.Login(context.Background(), "someone@test.io", "secret")

	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrInvalidCredentials)
}

func TestTheStoredCredentialIsNeverThePassword(t *testing.T) {
	var stored domain.User
	users := &userStub{create: func(u domain.User) (int64, error) {
		stored = u
		return 1, nil
	}}
	a := NewAuth(users, &tokenStub{}, bcryptTestCost)

	_, err := a.Register(context.Background(), "someone@test.io", "Someone", "secret")

	require.NoError(t, err)
	assert.NotContains(t, stored.PasswordHash, "secret")
	assert.NoError(t, compareHash(stored.PasswordHash, "secret"),
		"the stored hash must still verify the password it was made from")
}
