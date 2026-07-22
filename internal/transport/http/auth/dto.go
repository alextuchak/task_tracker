package auth

import (
	v "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"
)

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

func (r registerRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.Email, v.Required, is.Email),
		v.Field(&r.Name, v.Required, v.Length(1, 255)),
		v.Field(&r.Password, v.Required, v.Length(8, 72)),
	)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r loginRequest) Validate() error {
	return v.ValidateStruct(&r,
		v.Field(&r.Email, v.Required),
		v.Field(&r.Password, v.Required),
	)
}

// responses
type userResponse struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	ID    int64  `json:"id"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
}
