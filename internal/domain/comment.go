package domain

import "time"

type TaskComment struct {
	CreatedAt time.Time
	UpdatedAt time.Time
	Body      string
	ID        int64
	TaskID    int64
	UserID    int64
}

type CommentFilter struct {
	TaskID  int64
	Limit   int
	AfterID int64
}
