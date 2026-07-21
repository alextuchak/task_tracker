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

func (r *UserRepo) ByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, name, created_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user by email: %w", err)
	}
	return u, nil
}
