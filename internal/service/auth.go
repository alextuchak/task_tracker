package service

import (
	"context"
	"errors"
	"fmt"
	"task_tracker/internal/domain"

	"golang.org/x/crypto/bcrypt"
)

type UserRepository interface {
	Create(ctx context.Context, u domain.User) (int64, error)
	ByEmail(ctx context.Context, email string) (domain.User, error)
	ByID(ctx context.Context, id int64) (domain.User, error)
	GrantAdmin(ctx context.Context, email string) error
}

type TokenIssuer interface {
	Issue(userID int64) (string, error)
}

func NewAuth(users UserRepository, tokens TokenIssuer) *Auth {
	return &Auth{users: users, tokens: tokens}
}

type Auth struct {
	users  UserRepository
	tokens TokenIssuer
}

type User struct {
	Email string
	Name  string
	Role  domain.Role
	ID    int64
}

func (a *Auth) Register(ctx context.Context, email, name, password string) (User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	id, err := a.users.Create(ctx, domain.User{Email: email, Name: name, PasswordHash: string(hash)})
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return User{ID: id, Email: email, Name: name, Role: domain.RoleUser}, nil
}

func (a *Auth) UserByID(ctx context.Context, id int64) (User, error) {
	u, err := a.users.ByID(ctx, id)
	if err != nil {
		return User{}, fmt.Errorf("find user by id: %w", err)
	}
	return User{ID: u.ID, Email: u.Email, Name: u.Name, Role: u.Role}, nil
}

func (a *Auth) GrantAdmin(ctx context.Context, email string) error {
	if err := a.users.GrantAdmin(ctx, email); err != nil {
		return fmt.Errorf("grant admin: %w", err)
	}
	return nil
}

func (a *Auth) Login(ctx context.Context, email, password string) (string, error) {
	u, err := a.users.ByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return "", domain.ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return "", domain.ErrInvalidCredentials
	}
	t, err := a.tokens.Issue(u.ID)
	if err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}
	return t, nil
}
