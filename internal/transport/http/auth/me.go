package auth

import (
	"errors"
	"net/http"
	"task_tracker/internal/domain"
	"task_tracker/internal/identity"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/httpkit"
)

// Me godoc
//
//	@Summary	Current user
//	@Tags		auth
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{object}	userResponse
//	@Failure	401	{object}	httpkit.ErrorResponse	"missing or invalid token"
//	@Router		/me [get]
func Me(svc *service.Auth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		u, err := svc.UserByID(r.Context(), principal.UserID)
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusOK,
				userResponse{ID: u.ID, Email: u.Email, Name: u.Name, Role: string(u.Role)})
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusUnauthorized, "user no longer exists")
		default:
			httpkit.WriteInternalError(w, r, err)
		}
	}
}
