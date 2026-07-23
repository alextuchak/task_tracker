package tasks

import (
	"time"

	v "github.com/go-ozzo/ozzo-validation/v4"
)

var validStatuses = []any{"todo", "in_progress", "done"}

type createTaskRequest struct {
	AssigneeID  *int64 `json:"assignee_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	TeamID      int64  `json:"team_id"`
}

func (r createTaskRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.TeamID, v.Required, v.Min(1)),
		v.Field(&r.Title, v.Required, v.Length(1, 500)),
		v.Field(&r.Status, v.In(validStatuses...)),
	)
}

type updateTaskRequest struct {
	AssigneeID  *int64 `json:"assignee_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

func (r updateTaskRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.Title, v.Required, v.Length(1, 500)),
		v.Field(&r.Status, v.Required, v.In(validStatuses...)),
	)
}

// responses
type taskResponse struct {
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at"`
	AssigneeID  *int64     `json:"assignee_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	CreatedBy   int64      `json:"created_by"`
}

type taskListResponse struct {
	NextCursor *int64         `json:"next_cursor"`
	Items      []taskResponse `json:"items"`
}

type changeResponse struct {
	ChangedAt     time.Time `json:"changed_at"`
	ChangeGroupID string    `json:"change_group_id"`
	Field         string    `json:"field"`
	OldValue      string    `json:"old_value"`
	NewValue      string    `json:"new_value"`
	ChangedBy     int64     `json:"changed_by"`
}
