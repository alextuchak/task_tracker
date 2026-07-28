package comments

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
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

func Routes(svc *service.Comments) chi.Router {
	r := chi.NewRouter()
	r.Post("/", createHandler(svc))
	r.Get("/", listHandler(svc))
	r.Put("/{id}", updateHandler(svc))
	r.Delete("/{id}", deleteHandler(svc))
	return r
}

// createHandler godoc
//
//	@Summary	Leave a comment on a task (team members only)
//	@Tags		comments
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		request	body		createCommentRequest	true	"comment data"
//	@Success	201		{object}	commentResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid data"
//	@Failure	404		{object}	httpkit.ErrorResponse	"task not found or not a team member"
//	@Router		/comments [post]
func createHandler(svc *service.Comments) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		var req createCommentRequest
		if !httpkit.DecodeJSON(w, r, &req) {
			return
		}
		req.Body = strings.TrimSpace(req.Body)
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		comment, err := svc.Create(r.Context(), principal.UserID, req.TaskID, req.Body)
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusCreated, toCommentResponse(comment))
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, r, err)
		}
	}
}

// listHandler godoc
//
//	@Summary	Task comments, oldest first, with pagination
//	@Tags		comments
//	@Produce	json
//	@Security	BearerAuth
//	@Param		task_id	query		int	true	"task id"
//	@Param		limit	query		int	false	"page size (1..100, default 20)"
//	@Param		cursor	query		int	false	"last seen id from next_cursor; omit for the first page"
//	@Success	200		{object}	commentListResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid parameters"
//	@Failure	404		{object}	httpkit.ErrorResponse	"task not found or not a team member"
//	@Router		/comments [get]
func listHandler(svc *service.Comments) http.HandlerFunc {
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
		comments, err := svc.List(r.Context(), principal.UserID, filter)
		switch {
		case err == nil:
			resp := commentListResponse{Items: make([]commentResponse, 0, len(comments))}
			for _, c := range comments {
				resp.Items = append(resp.Items, toCommentResponse(c))
			}
			if len(comments) == filter.Limit {
				last := comments[len(comments)-1].ID
				resp.NextCursor = &last
			}
			httpkit.WriteJSON(w, http.StatusOK, resp)
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, r, err)
		}
	}
}

// updateHandler godoc
//
//	@Summary	Edit a comment (author or global admin)
//	@Tags		comments
//	@Accept		json
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id		path		int						true	"comment id"
//	@Param		request	body		updateCommentRequest	true	"new body"
//	@Success	200		{object}	commentResponse
//	@Failure	400		{object}	httpkit.ErrorResponse	"invalid data"
//	@Failure	403		{object}	httpkit.ErrorResponse	"not the author"
//	@Failure	404		{object}	httpkit.ErrorResponse	"comment not found or not a team member"
//	@Router		/comments/{id} [put]
func updateHandler(svc *service.Comments) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		commentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid comment id")
			return
		}
		var req updateCommentRequest
		if !httpkit.DecodeJSON(w, r, &req) {
			return
		}
		req.Body = strings.TrimSpace(req.Body)
		if err := req.Validate(); err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		comment, err := svc.Update(r.Context(), principal.UserID, commentID, req.Body)
		switch {
		case err == nil:
			httpkit.WriteJSON(w, http.StatusOK, toCommentResponse(comment))
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, r, err)
		}
	}
}

// deleteHandler godoc
//
//	@Summary	Delete a comment (author or global admin)
//	@Tags		comments
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path	int	true	"comment id"
//	@Success	204	"deleted"
//	@Failure	400	{object}	httpkit.ErrorResponse	"invalid comment id"
//	@Failure	403	{object}	httpkit.ErrorResponse	"not the author"
//	@Failure	404	{object}	httpkit.ErrorResponse	"comment not found or not a team member"
//	@Router		/comments/{id} [delete]
func deleteHandler(svc *service.Comments) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := identity.FromContext(r.Context())
		if !ok {
			httpkit.WriteError(w, http.StatusUnauthorized, "missing principal")
			return
		}
		commentID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			httpkit.WriteError(w, http.StatusBadRequest, "invalid comment id")
			return
		}
		err = svc.Delete(r.Context(), principal.UserID, commentID)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, domain.ErrNotFound):
			httpkit.WriteError(w, http.StatusNotFound, domain.ErrNotFound.Error())
		case errors.Is(err, domain.ErrForbidden):
			httpkit.WriteError(w, http.StatusForbidden, domain.ErrForbidden.Error())
		default:
			httpkit.WriteInternalError(w, r, err)
		}
	}
}

func parseListFilter(r *http.Request) (domain.CommentFilter, error) {
	q := r.URL.Query()
	taskID, err := strconv.ParseInt(q.Get("task_id"), 10, 64)
	if err != nil || taskID < 1 {
		return domain.CommentFilter{}, errors.New("task_id is required")
	}
	f := domain.CommentFilter{TaskID: taskID, Limit: defaultLimit}

	if l := q.Get("limit"); l != "" {
		limit, err := strconv.Atoi(l)
		if err != nil || limit < 1 || limit > maxLimit {
			return domain.CommentFilter{}, errors.New("limit must be 1..100")
		}
		f.Limit = limit
	}
	if c := q.Get("cursor"); c != "" {
		afterID, err := strconv.ParseInt(c, 10, 64)
		if err != nil || afterID < 1 {
			return domain.CommentFilter{}, errors.New("invalid cursor")
		}
		f.AfterID = afterID
	}
	return f, nil
}

func toCommentResponse(c domain.TaskComment) commentResponse {
	return commentResponse{
		ID:        c.ID,
		TaskID:    c.TaskID,
		UserID:    c.UserID,
		Body:      c.Body,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}
