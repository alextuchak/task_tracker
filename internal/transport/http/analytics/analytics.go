package analytics

import (
	"errors"
	"net/http"
	"task_tracker/internal/domain"
	"task_tracker/internal/identity"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/httpkit"

	"github.com/go-chi/chi/v5"
)

func Routes(svc *service.Analytics) chi.Router {
	r := chi.NewRouter()
	r.Get("/teams", teamStatsHandler(svc))
	r.Get("/top-creators", topCreatorsHandler(svc))
	r.Get("/orphan-assignees", orphanAssigneesHandler(svc))
	return r
}

// teamStatsHandler godoc
//
//	@Summary	Team stats: members and tasks done in the last 7 days (admin only)
//	@Tags		analytics
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{array}		teamStatsResponse
//	@Failure	403	{object}	httpkit.ErrorResponse	"not an admin"
//	@Router		/analytics/teams [get]
func teamStatsHandler(svc *service.Analytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		stats, err := svc.TeamStats(r.Context(), principal.UserID)
		switch {
		case err == nil:
			resp := make([]teamStatsResponse, 0, len(stats))
			for _, s := range stats {
				resp = append(resp, teamStatsResponse{
					ID: s.ID, Name: s.Name, Members: s.Members, DoneLast7Days: s.DoneLast7Days,
				})
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

// topCreatorsHandler godoc
//
//	@Summary	Top-3 task creators per team for the last month (admin only)
//	@Tags		analytics
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{array}		topCreatorResponse
//	@Failure	403	{object}	httpkit.ErrorResponse	"not an admin"
//	@Router		/analytics/top-creators [get]
func topCreatorsHandler(svc *service.Analytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		creators, err := svc.TopCreators(r.Context(), principal.UserID)
		switch {
		case err == nil:
			resp := make([]topCreatorResponse, 0, len(creators))
			for _, c := range creators {
				resp = append(resp, topCreatorResponse{
					TeamID: c.TeamID, TeamName: c.TeamName, UserID: c.UserID,
					UserName: c.UserName, TasksCreated: c.TasksCreated, Rank: c.Rank,
				})
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

// orphanAssigneesHandler godoc
//
//	@Summary	Tasks whose assignee is not a team member — integrity check (admin only)
//	@Tags		analytics
//	@Produce	json
//	@Security	BearerAuth
//	@Success	200	{array}		orphanAssigneeResponse
//	@Failure	403	{object}	httpkit.ErrorResponse	"not an admin"
//	@Router		/analytics/orphan-assignees [get]
func orphanAssigneesHandler(svc *service.Analytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		orphans, err := svc.OrphanAssignees(r.Context(), principal.UserID)
		switch {
		case err == nil:
			resp := make([]orphanAssigneeResponse, 0, len(orphans))
			for _, o := range orphans {
				resp = append(resp, orphanAssigneeResponse{
					TaskID: o.TaskID, TeamID: o.TeamID, AssigneeID: o.AssigneeID, Title: o.Title,
				})
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}
