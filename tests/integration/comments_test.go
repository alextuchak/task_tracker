package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type commentDTO struct {
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Body      string `json:"body"`
	ID        int64  `json:"id"`
	TaskID    int64  `json:"task_id"`
	UserID    int64  `json:"user_id"`
}

type commentPageDTO struct {
	NextCursor *int64       `json:"next_cursor"`
	Items      []commentDTO `json:"items"`
}

func leaveComment(t *testing.T, bearer string, taskID int64, body string) commentDTO {
	t.Helper()
	resp := doJSON(t, http.MethodPost, "/api/v1/comments", bearer,
		fmt.Sprintf(`{"task_id":%d,"body":%q}`, taskID, body))
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var c commentDTO
	decodeJSON(t, readBody(t, resp), &c)
	return c
}

func TestAuthorEditsAndDeletesOwnComment(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-author"))
	teamID := createTeam(t, owner, "c-author")
	task := createTask(t, owner, teamID, "task")

	comment := leaveComment(t, owner, task.ID, "first")
	require.Equal(t, "first", comment.Body)
	require.Equal(t, task.ID, comment.TaskID)
	require.Equal(t, comment.CreatedAt, comment.UpdatedAt)

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), owner,
		`{"body":"edited"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var updated commentDTO
	decodeJSON(t, readBody(t, resp), &updated)
	require.Equal(t, "edited", updated.Body)
	require.Equal(t, comment.ID, updated.ID)
	require.Equal(t, comment.CreatedAt, updated.CreatedAt)
	require.NotEmpty(t, updated.UpdatedAt)

	resp = doJSON(t, http.MethodDelete, fmt.Sprintf("/api/v1/comments/%d", comment.ID), owner, "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	resp = doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/comments?task_id=%d", task.ID), owner, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var page commentPageDTO
	decodeJSON(t, readBody(t, resp), &page)
	require.Empty(t, page.Items)
}

func TestMemberLeavesCommentVisibleToTeam(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-member-owner"))
	memberEmail := mail("c-member")
	registerAndLogin(t, memberEmail)
	teamID := createTeam(t, owner, "c-member")
	task := createTask(t, owner, teamID, "task")
	invite(t, owner, teamID, memberEmail)

	member := login(t, memberEmail, "password123")
	memberID := userID(t, member)
	comment := leaveComment(t, member, task.ID, "from member")
	require.Equal(t, memberID, comment.UserID)

	resp := doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/comments?task_id=%d", task.ID), owner, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var page commentPageDTO
	decodeJSON(t, readBody(t, resp), &page)
	require.Len(t, page.Items, 1)
	require.Equal(t, comment.ID, page.Items[0].ID)
	require.Equal(t, "from member", page.Items[0].Body)
	require.Equal(t, memberID, page.Items[0].UserID)
}

func TestOutsiderCannotSeeOrLeaveComments(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-out-owner"))
	outsider := registerAndLogin(t, mail("c-outsider"))
	teamID := createTeam(t, owner, "c-out")
	task := createTask(t, owner, teamID, "task")
	comment := leaveComment(t, owner, task.ID, "private")

	resp := doJSON(t, http.MethodPost, "/api/v1/comments", outsider,
		fmt.Sprintf(`{"task_id":%d,"body":"intruder"}`, task.ID))
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = doJSON(t, http.MethodGet, fmt.Sprintf("/api/v1/comments?task_id=%d", task.ID), outsider, "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), outsider,
		`{"body":"intruder"}`)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = doJSON(t, http.MethodDelete, fmt.Sprintf("/api/v1/comments/%d", comment.ID), outsider, "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
	require.JSONEq(t, `{"error":"not found"}`, readBody(t, resp))
}

func TestMemberEditsAndDeletesOwnComment(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-own-owner"))
	memberEmail := mail("c-own-member")
	registerAndLogin(t, memberEmail)
	teamID := createTeam(t, owner, "c-own")
	task := createTask(t, owner, teamID, "task")
	invite(t, owner, teamID, memberEmail)

	member := login(t, memberEmail, "password123")
	comment := leaveComment(t, member, task.ID, "mine")
	require.Equal(t, userID(t, member), comment.UserID)

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), member,
		`{"body":"mine, edited"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var updated commentDTO
	decodeJSON(t, readBody(t, resp), &updated)
	require.Equal(t, "mine, edited", updated.Body)

	resp = doJSON(t, http.MethodDelete, fmt.Sprintf("/api/v1/comments/%d", comment.ID), member, "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestTeamOwnerCannotModerateMemberComment(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-mod-owner"))
	memberEmail := mail("c-mod-member")
	registerAndLogin(t, memberEmail)
	teamID := createTeam(t, owner, "c-mod")
	task := createTask(t, owner, teamID, "task")
	invite(t, owner, teamID, memberEmail)

	member := login(t, memberEmail, "password123")
	comment := leaveComment(t, member, task.ID, "member's words")

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), owner,
		`{"body":"moderated by owner"}`)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp = doJSON(t, http.MethodDelete, fmt.Sprintf("/api/v1/comments/%d", comment.ID), owner, "")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCommentBodyIsStoredTrimmed(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-trim"))
	teamID := createTeam(t, owner, "c-trim")
	task := createTask(t, owner, teamID, "task")

	comment := leaveComment(t, owner, task.ID, "  padded  ")

	require.Equal(t, "padded", comment.Body)
}

func TestMemberCannotEditForeignComment(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-foreign-owner"))
	memberEmail := mail("c-foreign-member")
	registerAndLogin(t, memberEmail)
	teamID := createTeam(t, owner, "c-foreign")
	task := createTask(t, owner, teamID, "task")
	invite(t, owner, teamID, memberEmail)
	comment := leaveComment(t, owner, task.ID, "owner's words")

	member := login(t, memberEmail, "password123")

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), member,
		`{"body":"hijacked"}`)
	require.Equal(t, http.StatusForbidden, resp.StatusCode)

	resp = doJSON(t, http.MethodDelete, fmt.Sprintf("/api/v1/comments/%d", comment.ID), member, "")
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestGlobalAdminModeratesForeignComment(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-admin-owner"))
	teamID := createTeam(t, owner, "c-admin")
	task := createTask(t, owner, teamID, "task")
	comment := leaveComment(t, owner, task.ID, "needs moderation")

	admin := makeAdmin(t, mail("c-admin"))

	resp := doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), admin,
		`{"body":"moderated"}`)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var updated commentDTO
	decodeJSON(t, readBody(t, resp), &updated)
	require.Equal(t, "moderated", updated.Body)

	resp = doJSON(t, http.MethodDelete, fmt.Sprintf("/api/v1/comments/%d", comment.ID), admin, "")
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestCommentsPaginateByCursor(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-page"))
	teamID := createTeam(t, owner, "c-page")
	task := createTask(t, owner, teamID, "task")
	other := createTask(t, owner, teamID, "other task")
	ids := make([]int64, 0, 3)
	for i := range 3 {
		ids = append(ids, leaveComment(t, owner, task.ID, fmt.Sprintf("comment %d", i)).ID)
		leaveComment(t, owner, other.ID, fmt.Sprintf("noise %d", i))
	}

	resp := doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/comments?task_id=%d&limit=2", task.ID), owner, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var first commentPageDTO
	decodeJSON(t, readBody(t, resp), &first)
	require.Len(t, first.Items, 2)
	require.Equal(t, ids[0], first.Items[0].ID)
	require.Equal(t, ids[1], first.Items[1].ID)
	require.Equal(t, "comment 0", first.Items[0].Body)
	require.Equal(t, "comment 1", first.Items[1].Body)
	require.NotNil(t, first.NextCursor)
	require.Equal(t, ids[1], *first.NextCursor)

	resp = doJSON(t, http.MethodGet,
		fmt.Sprintf("/api/v1/comments?task_id=%d&limit=2&cursor=%d", task.ID, *first.NextCursor),
		owner, "")
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var second commentPageDTO
	decodeJSON(t, readBody(t, resp), &second)
	require.Len(t, second.Items, 1)
	require.Equal(t, ids[2], second.Items[0].ID)
	require.Equal(t, "comment 2", second.Items[0].Body)
	require.Nil(t, second.NextCursor)
}

func TestCommentCreateValidation(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-valid"))
	teamID := createTeam(t, owner, "c-valid")
	task := createTask(t, owner, teamID, "task")

	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing task_id", `{"body":"hi"}`, http.StatusBadRequest},
		{"zero task_id", `{"task_id":0,"body":"hi"}`, http.StatusBadRequest},
		{"empty body", fmt.Sprintf(`{"task_id":%d,"body":""}`, task.ID), http.StatusBadRequest},
		{"body of spaces only", fmt.Sprintf(`{"task_id":%d,"body":"   "}`, task.ID), http.StatusBadRequest},
		{"body at the 4000 limit", fmt.Sprintf(`{"task_id":%d,"body":%q}`, task.ID, strings.Repeat("a", 4000)), http.StatusCreated},
		{"body one over the limit", fmt.Sprintf(`{"task_id":%d,"body":%q}`, task.ID, strings.Repeat("a", 4001)), http.StatusBadRequest},
		{"broken json", `{"task_id":`, http.StatusBadRequest},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := doJSON(t, http.MethodPost, "/api/v1/comments", owner, c.body)

			require.Equal(t, c.wantCode, resp.StatusCode, readBody(t, resp))
		})
	}
}

func TestCommentListRejectsBadFilters(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-filter"))
	teamID := createTeam(t, owner, "c-filter")
	task := createTask(t, owner, teamID, "task")

	cases := []struct {
		name     string
		query    string
		wantCode int
	}{
		{"missing task_id", "", http.StatusBadRequest},
		{"non-numeric task_id", "?task_id=abc", http.StatusBadRequest},
		{"zero limit", fmt.Sprintf("?task_id=%d&limit=0", task.ID), http.StatusBadRequest},
		{"limit over the maximum", fmt.Sprintf("?task_id=%d&limit=101", task.ID), http.StatusBadRequest},
		{"non-numeric limit", fmt.Sprintf("?task_id=%d&limit=all", task.ID), http.StatusBadRequest},
		{"negative cursor", fmt.Sprintf("?task_id=%d&cursor=-1", task.ID), http.StatusBadRequest},
		{"limit at the maximum", fmt.Sprintf("?task_id=%d&limit=100", task.ID), http.StatusOK},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := doJSON(t, http.MethodGet, "/api/v1/comments"+c.query, owner, "")

			require.Equal(t, c.wantCode, resp.StatusCode, readBody(t, resp))
		})
	}
}

func TestCommentUpdateAndDeleteRejectBadRequests(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-badreq"))
	teamID := createTeam(t, owner, "c-badreq")
	task := createTask(t, owner, teamID, "task")
	comment := leaveComment(t, owner, task.ID, "body")

	resp := doJSON(t, http.MethodPut, "/api/v1/comments/abc", owner, `{"body":"hi"}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = doJSON(t, http.MethodDelete, "/api/v1/comments/abc", owner, "")
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), owner, `{"body":`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = doJSON(t, http.MethodPut, fmt.Sprintf("/api/v1/comments/%d", comment.ID), owner, `{"body":"  "}`)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	resp = doJSON(t, http.MethodPut, "/api/v1/comments/999999", owner, `{"body":"hi"}`)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = doJSON(t, http.MethodDelete, "/api/v1/comments/999999", owner, "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestCommentOnMissingTask(t *testing.T) {
	t.Parallel()
	owner := registerAndLogin(t, mail("c-missing"))

	resp := doJSON(t, http.MethodPost, "/api/v1/comments", owner, `{"task_id":999999,"body":"hi"}`)
	require.Equal(t, http.StatusNotFound, resp.StatusCode)

	resp = doJSON(t, http.MethodGet, "/api/v1/comments?task_id=999999", owner, "")
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
