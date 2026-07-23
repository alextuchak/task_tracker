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

	mysqldrv "github.com/go-sql-driver/mysql"
)

func NewTaskRepo(db *sql.DB) *TaskRepo {
	return &TaskRepo{db: db}
}

type TaskRepo struct {
	db *sql.DB
}

const taskColumns = `id, team_id, title, description, status, assignee_id,
	created_by, created_at, updated_at, completed_at`

func (r *TaskRepo) Create(ctx context.Context, t domain.Task) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO tasks (team_id, title, description, status, assignee_id, created_by)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.TeamID, t.Title, t.Description, t.Status, t.AssigneeID, t.CreatedBy)
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
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO task_history (task_id, changed_by, change_group_id, field, old_value, new_value)
		 VALUES (?, ?, ?, ?, NULL, NULL)`,
		id, t.CreatedBy, newChangeGroupID(), domain.ChangeFieldCreated); err != nil {
		return 0, fmt.Errorf("insert created event: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return id, nil
}

func (r *TaskRepo) ByID(ctx context.Context, id int64) (domain.Task, error) {
	return scanTask(r.db.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id))
}

func (r *TaskRepo) List(ctx context.Context, f domain.TaskFilter) ([]domain.Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE team_id = ?`
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

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tasks := make([]domain.Task, 0)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return tasks, nil
}

// history rows go in the same tx as the update, so the audit cannot drift
func (r *TaskRepo) Update(ctx context.Context, actorID int64, t domain.Task) (domain.Task, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	old, err := scanTask(tx.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ? FOR UPDATE`, t.ID))
	if err != nil {
		return domain.Task{}, err
	}

	var completedAt *time.Time
	switch {
	case t.Status == domain.TaskStatusDone && old.Status == domain.TaskStatusDone:
		completedAt = old.CompletedAt
	case t.Status == domain.TaskStatusDone:
		now := time.Now()
		completedAt = &now
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET title=?, description=?, status=?, assignee_id=?, completed_at=?
		 WHERE id=?`,
		t.Title, t.Description, t.Status, t.AssigneeID, completedAt, t.ID); err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}

	groupID := newChangeGroupID()
	for _, ch := range diffTask(old, t) {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO task_history (task_id, changed_by, change_group_id, field, old_value, new_value)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			t.ID, actorID, groupID, ch.Field, ch.OldValue, ch.NewValue); err != nil {
			return domain.Task{}, fmt.Errorf("insert history: %w", err)
		}
	}

	updated, err := scanTask(tx.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ?`, t.ID))
	if err != nil {
		return domain.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Task{}, fmt.Errorf("commit: %w", err)
	}
	return updated, nil
}

func (r *TaskRepo) History(ctx context.Context, taskID int64) ([]domain.TaskChange, error) {
	rows, err := r.db.QueryContext(ctx,
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

func diffTask(old, upd domain.Task) []domain.TaskChange {
	var changes []domain.TaskChange
	add := func(field, oldV, newV string) {
		if oldV != newV {
			changes = append(changes, domain.TaskChange{Field: field, OldValue: oldV, NewValue: newV})
		}
	}
	add("title", old.Title, upd.Title)
	add("description", old.Description, upd.Description)
	add("status", string(old.Status), string(upd.Status))
	add("assignee_id", formatID(old.AssigneeID), formatID(upd.AssigneeID))
	return changes
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

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTask(row rowScanner) (domain.Task, error) {
	var (
		t           domain.Task
		description sql.NullString
		assigneeID  sql.NullInt64
		completedAt sql.NullTime
	)
	err := row.Scan(&t.ID, &t.TeamID, &t.Title, &description, &t.Status, &assigneeID,
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
