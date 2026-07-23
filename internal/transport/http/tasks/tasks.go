package tasks

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

const (
	defaultLimit = 20
	maxLimit     = 100
)

func Routes(svc *service.Tasks) chi.Router {
	r := chi.NewRouter()
	r.Post("/", createHandler(svc))
	r.Get("/", listHandler(svc))
	r.Put("/{id}", updateHandler(svc))
	r.Get("/{id}/history", historyHandler(svc))
	return r
}

// createHandler godoc
//
//	@Summary	Create a task (team members only)
//	@Tags		tasks
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		createTaskRequest	true	"task data"
//	@Success	201		{object}	taskResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid data or assignee is not a team member"
//	@Failure	404		{object}	httpkit.ErrorResponse	"team not found"
//	@Router		/tasks [post]
func createHandler(svc *service.Tasks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		var req createTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		task, err := svc.Create(r.Context(), principal.UserID, req.TeamID, service.TaskInput{
			Title:       req.Title,
			Description: req.Description,
			Status:      domain.TaskStatus(req.Status),
			AssigneeID:  req.AssigneeID,
		})
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusCreated, toTaskResponse(task))
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		case errors.Is(err, domain.ErrNotTeamMember):
			httpkit.WriteError(w, http.StatusBadRequest, domain.ErrNotTeamMember.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

// listHandler godoc
//
//	@Summary	Team tasks with filters and pagination
//	@Tags		tasks
//	@Produce	json
//	@Security	BearerAuth
//	@Param		team_id		query		int		true	"team id"
//	@Param		status		query		string	false	"todo | in_progress | done"
//	@Param		assignee_id	query		int		false	"assignee id"
//	@Param		limit		query		int		false	"page size (1..100, default 20)"
//	@Param		cursor		query		int		false	"last seen id from next_cursor; omit for the first page"
//	@Success	200			{object}	taskListResponse
//	@Failure	400			{object}	httpkit.ErrorResponse	"invalid parameters"
//	@Failure	404			{object}	httpkit.ErrorResponse	"team not found"
//	@Router		/tasks [get]
func listHandler(svc *service.Tasks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		filter, err := parseListFilter(r)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		tasks, err := svc.List(r.Context(), principal.UserID, filter)
		switch {
		case err == nil:
			resp := taskListResponse{Items: make([]taskResponse, 0, len(tasks))}
			for _, t := range tasks {
				resp.Items = append(resp.Items, toTaskResponse(t))
			}
			if len(tasks) == filter.Limit {
				last := tasks[len(tasks)-1].ID
				resp.NextCursor = &last
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

// updateHandler godoc
//
//	@Summary	Update a task (team members only), changes are recorded in history
//	@Tags		tasks
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int					true	"task id"
//	@Param		request	body		updateTaskRequest	true	"new task state"
//	@Success	200		{object}	taskResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid data or assignee is not a team member"
//	@Failure	403		{object}	httpkit.ErrorResponse	"forbidden"
//	@Failure	404		{object}	httpkit.ErrorResponse	"task not found"
//	@Router		/tasks/{id} [put]
func updateHandler(svc *service.Tasks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid task id")
			return
		}
		var req updateTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		task, err := svc.Update(r.Context(), principal.UserID, taskID, service.TaskInput{
			Title:       req.Title,
			Description: req.Description,
			Status:      domain.TaskStatus(req.Status),
			AssigneeID:  req.AssigneeID,
		})
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusOK, toTaskResponse(task))
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		case errors.Is(err, domain.ErrNotTeamMember):
			httpkit.WriteError(w, http.StatusBadRequest, domain.ErrNotTeamMember.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

// historyHandler godoc
//
//	@Summary	Task change history
//	@Tags		tasks
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		int	true	"task id"
//	@Success	200	{array}		changeResponse
//	@Failure	404	{object}	httpkit.ErrorResponse	"task not found"
//	@Router		/tasks/{id}/history [get]
func historyHandler(svc *service.Tasks) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		taskID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid task id")
			return
		}
		changes, err := svc.History(r.Context(), principal.UserID, taskID)
		switch {
		case err == nil:
			resp := make([]changeResponse, 0, len(changes))
			for _, c := range changes {
				resp = append(resp, changeResponse{
					ChangeGroupID: c.GroupID,
					Field:         c.Field,
					OldValue:      c.OldValue,
					NewValue:      c.NewValue,
					ChangedBy:     c.ChangedBy,
					ChangedAt:     c.ChangedAt,
				})
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, err)
		}
	}
}

func parseListFilter(r *http.Request) (domain.TaskFilter, error) {
	q := r.URL.Query()
	teamID, err := strconv.ParseInt(q.Get("team_id"), 10, 64)
	if err != nil || teamID < 1 {
		return domain.TaskFilter{}, errors.New("team_id is required")
	}
	f := domain.TaskFilter{TeamID: teamID, Limit: defaultLimit}

	if s := q.Get("status"); s != "" {
		switch st := domain.TaskStatus(s); st {
		case domain.TaskStatusTodo, domain.TaskStatusInProgress, domain.TaskStatusDone:
			f.Status = &st
		default:
			return domain.TaskFilter{}, errors.New("invalid status")
		}
	}
	if a := q.Get("assignee_id"); a != "" {
		id, err := strconv.ParseInt(a, 10, 64)
		if err != nil || id < 1 {
			return domain.TaskFilter{}, errors.New("invalid assignee_id")
		}
		f.AssigneeID = &id
	}
	if l := q.Get("limit"); l != "" {
		limit, err := strconv.Atoi(l)
		if err != nil || limit < 1 || limit > maxLimit {
			return domain.TaskFilter{}, errors.New("limit must be 1..100")
		}
		f.Limit = limit
	}
	if c := q.Get("cursor"); c != "" {
		afterID, err := strconv.ParseInt(c, 10, 64)
		if err != nil || afterID < 1 {
			return domain.TaskFilter{}, errors.New("invalid cursor")
		}
		f.AfterID = afterID
	}
	return f, nil
}

func toTaskResponse(t domain.Task) taskResponse {
	return taskResponse{
		ID:          t.ID,
		TeamID:      t.TeamID,
		Title:       t.Title,
		Description: t.Description,
		Status:      string(t.Status),
		AssigneeID:  t.AssigneeID,
		CreatedBy:   t.CreatedBy,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		CompletedAt: t.CompletedAt,
	}
}
