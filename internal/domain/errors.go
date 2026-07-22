package domain

import "errors"

var (
	ErrEmailTaken         = errors.New("email already taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotFound           = errors.New("not found")
	ErrForbidden          = errors.New("forbidden")
	ErrAlreadyMember      = errors.New("already a member")
	ErrNotTeamMember      = errors.New("assignee is not a team member")
)
