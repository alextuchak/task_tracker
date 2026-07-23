package analytics

import (
	"errors"
	"net/http"
	"strconv"
	"task_tracker/internal/domain"
	"task_tracker/internal/identity"
	"task_tracker/internal/service"
	"task_tracker/internal/transport/http/httpkit"

	"github.com/go-chi/chi/v5"
)

const (
	defaultLimit = 20
	maxLimit     = 100
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
//	@Param		limit	query		int	false	"page size (1..100, default 20)"
//	@Param		cursor	query		int	false	"last seen team id from next_cursor; omit for the first page"
//	@Success	200		{object}	teamStatsListResponse
//	@Failure	403		{object}	httpkit.ErrorResponse	"not an admin"
//	@Router		/analytics/teams [get]
func teamStatsHandler(svc *service.Analytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		afterID, limit, err := parseCursorLimit(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		stats, err := svc.TeamStats(r.Context(), principal.UserID, afterID, limit)
		switch {
		case err == nil:
			resp := teamStatsListResponse{Items: make([]teamStatsResponse, 0, len(stats))}
			for _, s := range stats {
				resp.Items = append(resp.Items, teamStatsResponse{
					ID: s.ID, Name: s.Name, Members: s.Members, DoneLast7Days: s.DoneLast7Days,
				})
			}
			if len(stats) == limit {
				last := stats[len(stats)-1].ID
				resp.NextCursor = &last
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
//	@Param		limit	query		int	false	"teams per page (1..100, default 20)"
//	@Param		cursor	query		int	false	"last seen team id from next_cursor; omit for the first page"
//	@Success	200		{object}	topCreatorListResponse
//	@Failure	403		{object}	httpkit.ErrorResponse	"not an admin"
//	@Router		/analytics/top-creators [get]
func topCreatorsHandler(svc *service.Analytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		afterID, limit, err := parseCursorLimit(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		creators, err := svc.TopCreators(r.Context(), principal.UserID, afterID, limit)
		switch {
		case err == nil:
			resp := topCreatorListResponse{Items: make([]topCreatorResponse, 0, len(creators))}
			teams := 0
			var lastTeam int64
			for _, c := range creators {
				if c.TeamID != lastTeam {
					teams++
					lastTeam = c.TeamID
				}
				resp.Items = append(resp.Items, topCreatorResponse{
					TeamID: c.TeamID, TeamName: c.TeamName, UserID: c.UserID,
					UserName: c.UserName, TasksCreated: c.TasksCreated, Rank: c.Rank,
				})
			}
			if teams == limit {
				resp.NextCursor = &lastTeam
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
//	@Param		limit	query		int	false	"page size (1..100, default 20)"
//	@Param		cursor	query		int	false	"last seen task id from next_cursor; omit for the first page"
//	@Success	200		{object}	orphanAssigneeListResponse
//	@Failure	403		{object}	httpkit.ErrorResponse	"not an admin"
//	@Router		/analytics/orphan-assignees [get]
func orphanAssigneesHandler(svc *service.Analytics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		afterID, limit, err := parseCursorLimit(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		orphans, err := svc.OrphanAssignees(r.Context(), principal.UserID, afterID, limit)
		switch {
		case err == nil:
			resp := orphanAssigneeListResponse{Items: make([]orphanAssigneeResponse, 0, len(orphans))}
			for _, o := range orphans {
				resp.Items = append(resp.Items, orphanAssigneeResponse{
					TaskID: o.TaskID, TeamID: o.TeamID, AssigneeID: o.AssigneeID, Title: o.Title,
				})
			}
			if len(orphans) == limit {
				last := orphans[len(orphans)-1].TaskID
				resp.NextCursor = &last
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

func parseCursorLimit(r *http.Request) (afterID int64, limit int, err error) {
	q := r.URL.Query()
	limit = defaultLimit
	if l := q.Get("limit"); l != "" {
		limit, err = strconv.Atoi(l)
		if err != nil || limit < 1 || limit > maxLimit {
			return 0, 0, errors.New("limit must be 1..100")
		}
	}
	if c := q.Get("cursor"); c != "" {
		afterID, err = strconv.ParseInt(c, 10, 64)
		if err != nil || afterID < 1 {
			return 0, 0, errors.New("invalid cursor")
		}
	}
	return afterID, limit, nil
}
