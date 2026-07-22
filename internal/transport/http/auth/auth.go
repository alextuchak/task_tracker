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
//	@Summary	Регистрация пользователя
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		registerRequest	true	"данные регистрации"
//	@Success	201		{object}	userResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"невалидные данные"
//	@Failure	409		{object}	httpkit.ErrorResponse	"email занят"
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
//	@Summary	Аутентификация, выдаёт JWT
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		request	body		loginRequest	true	"email и пароль"
//	@Success	200		{object}	loginResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"невалидный json"
//	@Failure	401		{object}	httpkit.ErrorResponse	"неверные креды"
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
