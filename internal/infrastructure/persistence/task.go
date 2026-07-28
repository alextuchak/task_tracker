package persistence

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"task_tracker/internal/domain"
	"time"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
	mysqldrv "github.com/go-sql-driver/mysql"
)

func NewTaskRepo(db *sql.DB, getter *trmsql.CtxGetter) *TaskRepo {
	return &TaskRepo{db: db, getter: getter}
}

type TaskRepo struct {
	db     *sql.DB
	getter *trmsql.CtxGetter
}

func (r *TaskRepo) Create(ctx context.Context, t domain.Task) (int64, error) {
	conn := r.getter.DefaultTrOrDB(ctx, r.db)

	var completedAt *time.Time
	if t.Status == domain.TaskStatusDone {
		now := time.Now()
		completedAt = &now
	}
	res, err := conn.ExecContext(ctx,
		`INSERT INTO tasks (team_id, title, description, status, assignee_id, created_by, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.TeamID, t.Title, t.Description, t.Status, t.AssigneeID, t.CreatedBy, completedAt)
	var mysqlErr *mysqldrv.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == foreignKeyViolationCode {
		return 0, domain.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("insert task: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO task_history (task_id, changed_by, change_group_id, field, old_value, new_value)
		 VALUES (?, ?, ?, ?, NULL, NULL)`,
		id, t.CreatedBy, newChangeGroupID(), domain.ChangeFieldCreated); err != nil {
		return 0, fmt.Errorf("insert created event: %w", err)
	}
	return id, nil
}

func (r *TaskRepo) ByID(ctx context.Context, id int64) (domain.Task, error) {
	var (
		t           domain.Task
		description sql.NullString
		assigneeID  sql.NullInt64
		completedAt sql.NullTime
	)
	err := r.getter.DefaultTrOrDB(ctx, r.db).QueryRowContext(ctx,
		`SELECT id, team_id, title, description, status, assignee_id,
		        created_by, created_at, updated_at, completed_at
		   FROM tasks WHERE id = ?`, id).
		Scan(&t.ID, &t.TeamID, &t.Title, &description, &t.Status, &assigneeID,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &completedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Task{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Task{}, fmt.Errorf("scan task: %w", err)
	}
	t.Description = description.String
	if assigneeID.Valid {
		t.AssigneeID = &assigneeID.Int64
	}
	if completedAt.Valid {
		t.CompletedAt = &completedAt.Time
	}
	return t, nil
}

func (r *TaskRepo) List(ctx context.Context, f domain.TaskFilter) ([]domain.Task, error) {
	hint := ""
	switch {
	case f.AssigneeID != nil:
	case f.Status != nil:
		hint = ` FORCE INDEX (idx_tasks_team_status_id)`
	default:
		hint = ` FORCE INDEX (idx_tasks_team_id)`
	}
	query := `SELECT id, team_id, title, description, status, assignee_id,
	                 created_by, created_at, updated_at, completed_at
	            FROM tasks` + hint + ` WHERE team_id = ?`
	args := []any{f.TeamID}
	if f.Status != nil {
		query += ` AND status = ?`
		args = append(args, *f.Status)
	}
	if f.AssigneeID != nil {
		query += ` AND assignee_id = ?`
		args = append(args, *f.AssigneeID)
	}
	if f.AfterID > 0 {
		query += ` AND id > ?`
		args = append(args, f.AfterID)
	}
	query += ` ORDER BY id LIMIT ?`
	args = append(args, f.Limit)

	rows, err := r.getter.DefaultTrOrDB(ctx, r.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]domain.Task, 0)
	for rows.Next() {
		var (
			t           domain.Task
			description sql.NullString
			assigneeID  sql.NullInt64
			completedAt sql.NullTime
		)
		if err := rows.Scan(&t.ID, &t.TeamID, &t.Title, &description, &t.Status, &assigneeID,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		t.Description = description.String
		if assigneeID.Valid {
			t.AssigneeID = &assigneeID.Int64
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return tasks, nil
}

func (r *TaskRepo) Update(ctx context.Context, actorID int64, t domain.Task) (domain.Task, error) {
	conn := r.getter.DefaultTrOrDB(ctx, r.db)

	var (
		old          domain.Task
		oldDesc      sql.NullString
		oldAssignee  sql.NullInt64
		oldCompleted sql.NullTime
	)
	switch err := conn.QueryRowContext(ctx,
		`SELECT title, description, status, assignee_id, completed_at
		   FROM tasks WHERE id = ? FOR UPDATE`, t.ID).
		Scan(&old.Title, &oldDesc, &old.Status, &oldAssignee, &oldCompleted); {
	case errors.Is(err, sql.ErrNoRows):
		return domain.Task{}, domain.ErrNotFound
	case err != nil:
		return domain.Task{}, fmt.Errorf("lock task: %w", err)
	}
	old.Description = oldDesc.String
	if oldAssignee.Valid {
		old.AssigneeID = &oldAssignee.Int64
	}
	if oldCompleted.Valid {
		old.CompletedAt = &oldCompleted.Time
	}

	var completedAt *time.Time
	switch {
	case t.Status == domain.TaskStatusDone && old.Status == domain.TaskStatusDone:
		completedAt = old.CompletedAt
	case t.Status == domain.TaskStatusDone:
		now := time.Now()
		completedAt = &now
	}
	if _, err := conn.ExecContext(ctx,
		`UPDATE tasks SET title=?, description=?, status=?, assignee_id=?, completed_at=?
		 WHERE id=?`,
		t.Title, t.Description, t.Status, t.AssigneeID, completedAt, t.ID); err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}

	changes := make([]domain.TaskChange, 0, 4)
	changes = addChange(changes, "title", old.Title, t.Title)
	changes = addChange(changes, "description", old.Description, t.Description)
	changes = addChange(changes, "status", string(old.Status), string(t.Status))
	changes = addChange(changes, "assignee_id", formatID(old.AssigneeID), formatID(t.AssigneeID))

	groupID := newChangeGroupID()
	for _, ch := range changes {
		if _, err := conn.ExecContext(ctx,
			`INSERT INTO task_history (task_id, changed_by, change_group_id, field, old_value, new_value)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			t.ID, actorID, groupID, ch.Field, ch.OldValue, ch.NewValue); err != nil {
			return domain.Task{}, fmt.Errorf("insert history: %w", err)
		}
	}

	var (
		updated     domain.Task
		newDesc     sql.NullString
		newAssignee sql.NullInt64
		newDone     sql.NullTime
	)
	if err := conn.QueryRowContext(ctx,
		`SELECT id, team_id, title, description, status, assignee_id,
		        created_by, created_at, updated_at, completed_at
		   FROM tasks WHERE id = ?`, t.ID).
		Scan(&updated.ID, &updated.TeamID, &updated.Title, &newDesc, &updated.Status,
			&newAssignee, &updated.CreatedBy, &updated.CreatedAt, &updated.UpdatedAt,
			&newDone); err != nil {
		return domain.Task{}, fmt.Errorf("scan updated task: %w", err)
	}
	updated.Description = newDesc.String
	if newAssignee.Valid {
		updated.AssigneeID = &newAssignee.Int64
	}
	if newDone.Valid {
		updated.CompletedAt = &newDone.Time
	}
	return updated, nil
}

func (r *TaskRepo) History(ctx context.Context, taskID int64) ([]domain.TaskChange, error) {
	rows, err := r.getter.DefaultTrOrDB(ctx, r.db).QueryContext(ctx,
		`SELECT change_group_id, field, old_value, new_value, changed_by, changed_at
		 FROM task_history WHERE task_id = ? ORDER BY id`, taskID)
	if err != nil {
		return nil, fmt.Errorf("select history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	changes := make([]domain.TaskChange, 0)
	for rows.Next() {
		var (
			ch       domain.TaskChange
			oldValue sql.NullString
			newValue sql.NullString
		)
		if err := rows.Scan(&ch.GroupID, &ch.Field, &oldValue, &newValue, &ch.ChangedBy, &ch.ChangedAt); err != nil {
			return nil, fmt.Errorf("scan change: %w", err)
		}
		ch.OldValue, ch.NewValue = oldValue.String, newValue.String
		changes = append(changes, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return changes, nil
}

// addChange appends a history row only when the field actually moved.
func addChange(changes []domain.TaskChange, field, oldV, newV string) []domain.TaskChange {
	if oldV == newV {
		return changes
	}
	return append(changes, domain.TaskChange{Field: field, OldValue: oldV, NewValue: newV})
}

func newChangeGroupID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func formatID(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}
