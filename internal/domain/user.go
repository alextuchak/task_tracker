package domain

import "time"

type User struct {
	CreatedAt    time.Time
	Email        string
	Name         string
	PasswordHash string
	ID           int64
}
