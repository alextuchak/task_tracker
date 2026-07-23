package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"task_tracker/internal/domain"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/httpkit"

	"github.com/go-chi/chi/v5"
)

func Routes(svc *service.Auth) chi.Router {
	r := chi.NewRouter()
	r.Post("/register", registerHandler(svc))
	r.Post("/login", loginHandler(svc))
	return r
}

// registerHandler godoc
//
//	@Summary	Register a new user
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		registerRequest	true	"registration data"
//	@Success	201		{object}	userResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid data"
//	@Failure	409		{object}	httpkit.ErrorResponse	"email already taken"
//	@Router		/register [post]
func registerHandler(svc *service.Auth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		u, err := svc.Register(r.Context(), req.Email, req.Name, req.Password)
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusCreated,
				userResponse{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role)})
		case errors.Is(err, domain.ErrEmailTaken):
			httpkit.WriteError(w, http.StatusConflict, domain.ErrEmailTaken.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

// loginHandler godoc
//
//	@Summary	Authenticate and issue a JWT
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		loginRequest	true	"email and password"
//	@Success	200		{object}	loginResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid json"
//	@Failure	401		{object}	httpkit.ErrorResponse	"invalid credentials"
//	@Router		/login [post]
func loginHandler(svc *service.Auth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		t, err := svc.Login(r.Context(), req.Email, req.Password)
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusOK, loginResponse{AccessToken: t})
		case errors.Is(err, domain.ErrInvalidCredentials):
			httpkit.WriteError(w, http.StatusUnauthorized, domain.ErrInvalidCredentials.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}
