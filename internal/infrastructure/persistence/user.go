package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"task_tracker/internal/domain"

	mysqldrv "github.com/go-sql-driver/mysql"
)

const duplicateEntryCode = 1062

func NewUserRepo(db *sql.DB) *UserRepo {
	return &UserRepo{db: db}
}

type UserRepo struct {
	db *sql.DB
}

func (r *UserRepo) Create(ctx context.Context, u domain.User) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, name) VALUES (?, ?, ?)`,
		u.Email, u.PasswordHash, u.Name,
	)
	if err != nil {
		var mysqlErr *mysqldrv.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == duplicateEntryCode {
			return 0, domain.ErrEmailTaken
		}
		return 0, fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	return id, nil
}

func (r *UserRepo) ByID(ctx context.Context, id int64) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, name, role, created_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user by id: %w", err)
	}
	return u, nil
}

func (r *UserRepo) GrantAdmin(ctx context.Context, email string) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE users SET role = ? WHERE email = ?`, domain.RoleAdmin, email)
	if err != nil {
		return fmt.Errorf("grant admin: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if affected == 0 {
		if _, err := r.ByEmail(ctx, email); err != nil {
			return err
		}
	}
	return nil
}

func (r *UserRepo) ByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, role, created_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user by email: %w", err)
	}
	return u, nil
}
