package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"task_tracker/internal/domain"
)

func NewAnalyticsRepo(db *sql.DB) *AnalyticsRepo {
	return &AnalyticsRepo{db: db}
}

type AnalyticsRepo struct {
	db *sql.DB
}


func (r *AnalyticsRepo) TeamStats(ctx context.Context, afterID int64, limit int) ([]domain.TeamStats, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id,
		       t.name,
		       COALESCE(m.members, 0)      AS members,
		       COALESCE(d.done_last_7d, 0) AS done_last_7d
		FROM teams t
		LEFT JOIN (
		    SELECT team_id, COUNT(*) AS members
		    FROM team_members
		    GROUP BY team_id
		) m ON m.team_id = t.id
		LEFT JOIN (
		    SELECT team_id, COUNT(*) AS done_last_7d
		    FROM tasks
		    WHERE status = 'done' AND completed_at >= NOW() - INTERVAL 7 DAY
		    GROUP BY team_id
		) d ON d.team_id = t.id
		WHERE t.id > ?
		ORDER BY t.id
		LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("team stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := make([]domain.TeamStats, 0)
	for rows.Next() {
		var s domain.TeamStats
		if err := rows.Scan(&s.ID, &s.Name, &s.Members, &s.DoneLast7Days); err != nil {
			return nil, fmt.Errorf("scan team stats: %w", err)
		}
		stats = append(stats, s)
	}
	return stats, rows.Err()
}


func (r *AnalyticsRepo) TopCreators(ctx context.Context, afterID int64, limit int) ([]domain.TeamTopCreator, error) {
	rows, err := r.db.QueryContext(ctx, `
		WITH page_teams AS (
		    SELECT DISTINCT ta.team_id
		    FROM tasks ta
		    WHERE ta.created_at >= NOW() - INTERVAL 1 MONTH AND ta.team_id > ?
		    ORDER BY ta.team_id
		    LIMIT ?
		),
		created AS (
		    SELECT ta.team_id, ta.created_by, COUNT(*) AS cnt
		    FROM tasks ta
		    JOIN page_teams p ON p.team_id = ta.team_id
		    WHERE ta.created_at >= NOW() - INTERVAL 1 MONTH
		    GROUP BY ta.team_id, ta.created_by
		),
		ranked AS (
		    SELECT c.team_id, c.created_by, c.cnt,
		           ROW_NUMBER() OVER (
		               PARTITION BY c.team_id
		               ORDER BY c.cnt DESC, c.created_by
		           ) AS rn
		    FROM created c
		)
		SELECT r.team_id, t.name, r.created_by, u.name, r.cnt, r.rn
		FROM ranked r
		JOIN teams t ON t.id = r.team_id
		JOIN users u ON u.id = r.created_by
		WHERE r.rn <= 3
		ORDER BY r.team_id, r.rn`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("top creators: %w", err)
	}
	defer func() { _ = rows.Close() }()

	creators := make([]domain.TeamTopCreator, 0)
	for rows.Next() {
		var c domain.TeamTopCreator
		if err := rows.Scan(&c.TeamID, &c.TeamName, &c.UserID, &c.UserName,
			&c.TasksCreated, &c.Rank); err != nil {
			return nil, fmt.Errorf("scan top creator: %w", err)
		}
		creators = append(creators, c)
	}
	return creators, rows.Err()
}

func (r *AnalyticsRepo) OrphanAssignees(ctx context.Context, afterID int64, limit int) ([]domain.OrphanAssignee, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT ta.id, ta.team_id, ta.assignee_id, ta.title
		FROM tasks ta
		LEFT JOIN team_members tm
		       ON tm.team_id = ta.team_id AND tm.user_id = ta.assignee_id
		WHERE ta.assignee_id IS NOT NULL
		  AND tm.user_id IS NULL
		  AND ta.id > ?
		ORDER BY ta.id
		LIMIT ?`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("orphan assignees: %w", err)
	}
	defer func() { _ = rows.Close() }()

	orphans := make([]domain.OrphanAssignee, 0)
	for rows.Next() {
		var o domain.OrphanAssignee
		if err := rows.Scan(&o.TaskID, &o.TeamID, &o.AssigneeID, &o.Title); err != nil {
			return nil, fmt.Errorf("scan orphan: %w", err)
		}
		orphans = append(orphans, o)
	}
	return orphans, rows.Err()
}
