package service

import (
	"context"
	"task_tracker/internal/domain"
	"task_tracker/internal/infrastructure/outbox"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

type calls struct{ seen []string }

func (c *calls) add(name string) { c.seen = append(c.seen, name) }

func (c *calls) has(name string) bool {
	for _, s := range c.seen {
		if s == name {
			return true
		}
	}
	return false
}

type userStub struct {
	calls
	byID    func(id int64) (domain.User, error)
	byEmail func(email string) (domain.User, error)
	create  func(u domain.User) (int64, error)
}

func (s *userStub) Create(_ context.Context, u domain.User) (int64, error) {
	s.add("users.Create")
	if s.create == nil {
		return 0, nil
	}
	return s.create(u)
}

func (s *userStub) ByEmail(_ context.Context, email string) (domain.User, error) {
	s.add("users.ByEmail")
	if s.byEmail == nil {
		return domain.User{}, nil
	}
	return s.byEmail(email)
}

func (s *userStub) ByID(_ context.Context, id int64) (domain.User, error) {
	s.add("users.ByID")
	if s.byID == nil {
		return domain.User{}, nil
	}
	return s.byID(id)
}

func (s *userStub) GrantAdmin(_ context.Context, email string) error {
	s.add("users.GrantAdmin")
	return nil
}

type teamStub struct {
	calls
	memberRole func(teamID, userID int64) (domain.TeamRole, error)
}

func (s *teamStub) Create(_ context.Context, name string, creatorID int64) (int64, error) {
	s.add("teams.Create")
	return 0, nil
}

func (s *teamStub) ListByUser(_ context.Context, userID int64) ([]domain.TeamMembership, error) {
	s.add("teams.ListByUser")
	return nil, nil
}

func (s *teamStub) MemberRole(_ context.Context, teamID, userID int64) (domain.TeamRole, error) {
	s.add("teams.MemberRole")
	if s.memberRole == nil {
		return "", domain.ErrNotFound
	}
	return s.memberRole(teamID, userID)
}

func memberOf() *teamStub {
	return &teamStub{memberRole: func(int64, int64) (domain.TeamRole, error) {
		return domain.TeamRoleOwner, nil
	}}
}

func (s *teamStub) AddMember(_ context.Context, teamID, userID int64, role domain.TeamRole) error {
	s.add("teams.AddMember")
	return nil
}

type taskStub struct {
	calls
	list func(f domain.TaskFilter) ([]domain.Task, error)
	byID func(id int64) (domain.Task, error)
}

func (s *taskStub) Create(_ context.Context, t domain.Task) (int64, error) {
	s.add("tasks.Create")
	return 0, nil
}

func (s *taskStub) ByID(_ context.Context, id int64) (domain.Task, error) {
	s.add("tasks.ByID")
	if s.byID == nil {
		return domain.Task{}, nil
	}
	return s.byID(id)
}

func (s *taskStub) List(_ context.Context, f domain.TaskFilter) ([]domain.Task, error) {
	s.add("tasks.List")
	if s.list == nil {
		return nil, nil
	}
	return s.list(f)
}

func (s *taskStub) Update(_ context.Context, actorID int64, t domain.Task) (domain.Task, error) {
	s.add("tasks.Update")
	return t, nil
}

func (s *taskStub) History(_ context.Context, taskID int64) ([]domain.TaskChange, error) {
	s.add("tasks.History")
	return nil, nil
}

type cacheStub struct {
	calls
	onInvalidate func()
	bumped       []int64
}

func (s *cacheStub) GetList(_ context.Context, f domain.TaskFilter) ([]domain.Task, int64, bool) {
	s.add("cache.GetList")
	return nil, 0, false
}

func (s *cacheStub) SetList(context.Context, domain.TaskFilter, int64, []domain.Task) {
	s.add("cache.SetList")
}

func (s *cacheStub) InvalidateTeam(_ context.Context, teamID int64) {
	s.add("cache.InvalidateTeam")
	s.bumped = append(s.bumped, teamID)
	if s.onInvalidate != nil {
		s.onInvalidate()
	}
}

type outboxStub struct {
	calls
	err error
}

func (s *outboxStub) Enqueue(context.Context, outbox.Message) error {
	s.add("outbox.Enqueue")
	return s.err
}

type txStub struct {
	calls
	committed bool
}

func (s *txStub) Do(ctx context.Context, fn func(context.Context) error) error {
	s.add("tx.Do")
	err := fn(ctx)
	s.committed = err == nil
	return err
}

type tokenStub struct {
	calls
	err error
}

func (s *tokenStub) Issue(int64) (string, error) {
	s.add("tokens.Issue")
	return "token", s.err
}

const bcryptTestCost = bcrypt.MinCost

func hashFor(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptTestCost)
	require.NoError(t, err)
	return string(h)
}

func compareHash(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

type commentStub struct {
	calls
	byID func(id int64) (domain.TaskComment, error)
}

func (s *commentStub) Create(_ context.Context, c domain.TaskComment) (int64, error) {
	s.add("comments.Create")
	return 0, nil
}

func (s *commentStub) ByID(_ context.Context, id int64) (domain.TaskComment, error) {
	s.add("comments.ByID")
	if s.byID == nil {
		return domain.TaskComment{}, nil
	}
	return s.byID(id)
}

func (s *commentStub) List(_ context.Context, f domain.CommentFilter) ([]domain.TaskComment, error) {
	s.add("comments.List")
	return nil, nil
}

func (s *commentStub) Update(_ context.Context, _ int64, body string) (domain.TaskComment, error) {
	s.add("comments.Update")
	return domain.TaskComment{Body: body}, nil
}

func (s *commentStub) Delete(context.Context, int64) error {
	s.add("comments.Delete")
	return nil
}
