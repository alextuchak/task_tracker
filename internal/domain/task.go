package domain

import "time"

type TaskStatus string

const (
	TaskStatusTodo       TaskStatus = "todo"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusDone       TaskStatus = "done"
)

type Task struct {
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
	AssigneeID  *int64
	Title       string
	Description string
	Status      TaskStatus
	ID          int64
	TeamID      int64
	CreatedBy   int64
}

// ChangeFieldCreated — событие рождения задачи в истории.
const ChangeFieldCreated = "created"

type TaskChange struct {
	ChangedAt time.Time
	GroupID   string
	Field     string
	OldValue  string
	NewValue  string
	ChangedBy int64
}

type TaskFilter struct {
	Status     *TaskStatus
	AssigneeID *int64
	TeamID     int64
	Limit      int
	AfterID    int64
}
