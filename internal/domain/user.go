package domain

import "time"

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

type User struct {
	CreatedAt    time.Time
	Email        string
	Name         string
	PasswordHash string
	Role         Role
	ID           int64
}
