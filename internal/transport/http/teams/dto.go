package teams

import (
	v "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

type createTeamRequest struct {
	Name string `json:"name"`
}

func (r createTeamRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.Name, v.Required, v.Length(1, 255)),
	)
}

type inviteRequest struct {
	Email string `json:"email"`
}

func (r inviteRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.Email, v.Required, is.EmailFormat),
	)
}

// responses
type teamResponse struct {
	Name string `json:"name"`
	Role string `json:"role"`
	ID   int64  `json:"id"`
}
