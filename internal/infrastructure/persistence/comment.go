package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"task_tracker/internal/domain"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
)

func NewCommentRepo(db *sql.DB, getter *trmsql.CtxGetter) *CommentRepo {
	return &CommentRepo{db: db, getter: getter}
}

type CommentRepo struct {
	db     *sql.DB
	getter *trmsql.CtxGetter
}

func (r *CommentRepo) Create(ctx context.Context, c domain.TaskComment) (int64, error) {
	res, err := r.getter.DefaultTrOrDB(ctx, r.db).ExecContext(ctx,
		`INSERT INTO task_comments (task_id, user_id, body) VALUES (?, ?, ?)`,
		c.TaskID, c.UserID, c.Body)
	if err != nil {
		return 0, fmt.Errorf("insert comment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

func (r *CommentRepo) ByID(ctx context.Context, id int64) (domain.TaskComment, error) {
	var c domain.TaskComment
	err := r.getter.DefaultTrOrDB(ctx, r.db).QueryRowContext(ctx,
		`SELECT id, task_id, user_id, body, created_at, updated_at
		   FROM task_comments WHERE id = ?`, id).
		Scan(&c.ID, &c.TaskID, &c.UserID, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskComment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskComment{}, fmt.Errorf("scan comment: %w", err)
	}
	return c, nil
}

func (r *CommentRepo) List(ctx context.Context, f domain.CommentFilter) ([]domain.TaskComment, error) {
	query := `SELECT id, task_id, user_id, body, created_at, updated_at
	            FROM task_comments WHERE task_id = ?`
	args := []any{f.TaskID}
	if f.AfterID > 0 {
		query += ` AND id > ?`
		args = append(args, f.AfterID)
	}
	query += ` ORDER BY id LIMIT ?`
	args = append(args, f.Limit)

	rows, err := r.getter.DefaultTrOrDB(ctx, r.db).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("select comments: %w", err)
	}
	defer func() { _ = rows.Close() }()

	comments := make([]domain.TaskComment, 0)
	for rows.Next() {
		var c domain.TaskComment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.UserID, &c.Body, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan comment: %w", err)
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return comments, nil
}

func (r *CommentRepo) Update(ctx context.Context, id int64, body string) (domain.TaskComment, error) {
	conn := r.getter.DefaultTrOrDB(ctx, r.db)
	if _, err := conn.ExecContext(ctx,
		`UPDATE task_comments SET body = ? WHERE id = ?`, body, id); err != nil {
		return domain.TaskComment{}, fmt.Errorf("update comment: %w", err)
	}

	var c domain.TaskComment
	err := conn.QueryRowContext(ctx,
		`SELECT id, task_id, user_id, body, created_at, updated_at
		   FROM task_comments WHERE id = ?`, id).
		Scan(&c.ID, &c.TaskID, &c.UserID, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TaskComment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.TaskComment{}, fmt.Errorf("scan updated comment: %w", err)
	}
	return c, nil
}

func (r *CommentRepo) Delete(ctx context.Context, id int64) error {
	if _, err := r.getter.DefaultTrOrDB(ctx, r.db).ExecContext(ctx,
		`DELETE FROM task_comments WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	return nil
}
