package teams

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"task_tracker/internal/domain"
	"task_tracker/internal/identity"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/httpkit"

	"github.com/go-chi/chi/v5"
)

func Routes(svc *service.Teams) chi.Router {
	r := chi.NewRouter()
	r.Post("/", createHandler(svc))
	r.Get("/", listHandler(svc))
	r.Post("/{id}/invite", inviteHandler(svc))
	return r
}

// createHandler godoc
//
//	@Summary	Create a team, creator becomes owner
//	@Tags		teams
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		createTeamRequest	true	"team name"
//	@Success	201		{object}	teamResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid data"
//	@Failure	401		{object}	httpkit.ErrorResponse	"missing token"
//	@Router		/teams [post]
func createHandler(svc *service.Teams) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		var req createTeamRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		m, err := svc.Create(r.Context(), principal.UserID, req.Name)
		if err != nil {
			httpkit.WriteInternalError(w, err)
			return
		}
		httpkit.WriteJSON(w, http.StatusCreated,
			teamResponse{ID: m.ID, Name: m.Name, Role: string(m.Role)})
	}
}

// listHandler godoc
//
//	@Summary	Teams the current user belongs to
//	@Tags		teams
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{array}		teamResponse
//	@Failure	401	{object}	httpkit.ErrorResponse	"missing token"
//	@Router		/teams [get]
func listHandler(svc *service.Teams) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		memberships, err := svc.List(r.Context(), principal.UserID)
		if err != nil {
			httpkit.WriteInternalError(w, err)
			return
		}
		resp := make([]teamResponse, 0, len(memberships))
		for _, m := range memberships {
			resp = append(resp, teamResponse{ID: m.ID, Name: m.Name, Role: string(m.Role)})
		}
		httpkit.WriteJSON(w, http.StatusOK, resp)
	}
}

// inviteHandler godoc
//
//	@Summary	Invite a user to a team (team owner/admin or global admin)
//	@Tags		teams
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path	int				true	"team id"
//	@Param		request	body	inviteRequest	true	"invitee email"
//	@Success	204
//	@Failure	400	{object}	httpkit.ErrorResponse	"invalid data"
//	@Failure	403	{object}	httpkit.ErrorResponse	"forbidden"
//	@Failure	404	{object}	httpkit.ErrorResponse	"team or user not found"
//	@Failure	409	{object}	httpkit.ErrorResponse	"already a member"
//	@Router		/teams/{id}/invite [post]
func inviteHandler(svc *service.Teams) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		teamID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid team id")
			return
		}
		var req inviteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		err = svc.Invite(r.Context(), principal.UserID, teamID, req.Email)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		case errors.Is(err, domain.ErrAlreadyMember):
			httpkit.WriteError(w, http.StatusConflict, domain.ErrAlreadyMember.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}
