package auth

import (
	"net/http"
	"task_tracker/internal/identity"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/httpkit"
)

// Me godoc
//
//	@Summary	Текущий пользователь
//	@Tags		auth
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	userResponse
//	@Failure	401	{object}	httpkit.ErrorResponse	"нет или невалидный токен"
//	@Router		/me [get]
func Me(svc *service.Auth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		u, err := svc.UserByID(r.Context(), principal.UserID)
		if err != nil {
			httpkit.WriteInternalError(w, err)
			return
		}
		httpkit.WriteJSON(w, http.StatusOK,
			userResponse{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role)})
	}
}
