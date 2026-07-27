package comments

import (
	"time"

	v "github.com/go-ozzo/ozzo-validation/v4"
)

const maxBodyLen = 4000

type createCommentRequest struct {
	Body   string `json:"body"`
	TaskID int64  `json:"task_id"`
}

func (r createCommentRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.TaskID, v.Required, v.Min(1)),
		v.Field(&r.Body, v.Required, v.Length(1, maxBodyLen)),
	)
}

type updateCommentRequest struct {
	Body string `json:"body"`
}

func (r updateCommentRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.Body, v.Required, v.Length(1, maxBodyLen)),
	)
}

// responses
type commentResponse struct {
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Body      string    `json:"body"`
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
}

type commentListResponse struct {
	NextCursor *int64            `json:"next_cursor"`
	Items      []commentResponse `json:"items"`
}
