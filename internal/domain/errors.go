package domain

import "errors"

var (
	ErrEmailTaken         = errors.New("email already taken")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrNotFound           = errors.New("not found")
)
