package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"task_tracker/internal/domain"

	mysqldrv "github.com/go-sql-driver/mysql"
)

const foreignKeyViolationCode = 1452

func NewTeamRepo(db *sql.DB) *TeamRepo {
	return &TeamRepo{db: db}
}

type TeamRepo struct {
	db *sql.DB
}

func (r *TeamRepo) Create(ctx context.Context, name string, creatorID int64) (int64, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO teams (name, created_by) VALUES (?, ?)`, name, creatorID)
	if err != nil {
		return 0, fmt.Errorf("insert team: %w", err)
	}
	teamID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("last insert id: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`,
		teamID, creatorID, domain.TeamRoleOwner); err != nil {
		return 0, fmt.Errorf("insert owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return teamID, nil
}

func (r *TeamRepo) ListByUser(ctx context.Context, userID int64) ([]domain.TeamMembership, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT t.id, t.name, tm.role
		 FROM team_members tm
		 JOIN teams t ON t.id = tm.team_id
		 WHERE tm.user_id = ?
		 ORDER BY t.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("select teams by user: %w", err)
	}
	defer func() { _ = rows.Close() }()

	memberships := make([]domain.TeamMembership, 0)
	for rows.Next() {
		var m domain.TeamMembership
		if err := rows.Scan(&m.ID, &m.Name, &m.Role); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		memberships = append(memberships, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return memberships, nil
}

func (r *TeamRepo) MemberRole(ctx context.Context, teamID, userID int64) (domain.TeamRole, error) {
	var role domain.TeamRole
	err := r.db.QueryRowContext(ctx,
		`SELECT role FROM team_members WHERE team_id = ? AND user_id = ?`,
		teamID, userID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("select member role: %w", err)
	}
	return role, nil
}

func (r *TeamRepo) AddMember(ctx context.Context, teamID, userID int64, role domain.TeamRole) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, ?)`,
		teamID, userID, role)
	var mysqlErr *mysqldrv.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case duplicateEntryCode:
			return domain.ErrAlreadyMember
		case foreignKeyViolationCode:
			return domain.ErrNotFound
		}
	}
	if err != nil {
		return fmt.Errorf("insert member: %w", err)
	}
	return nil
}
