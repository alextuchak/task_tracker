package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"task_tracker/internal/infrastructure/outbox"
	"time"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
)

const maxErrLen = 1024

func NewOutboxRepo(db *sql.DB, getter *trmsql.CtxGetter) *OutboxRepo {
	return &OutboxRepo{db: db, getter: getter}
}

type OutboxRepo struct {
	db     *sql.DB
	getter *trmsql.CtxGetter
}

func (r *OutboxRepo) Enqueue(ctx context.Context, msg outbox.Message) error {
	conn := r.getter.DefaultTrOrDB(ctx, r.db)
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO email_outbox (recipient, subject, body) VALUES (?, ?, ?)`,
		msg.Recipient, msg.Subject, msg.Body); err != nil {
		return fmt.Errorf("insert outbox: %w", err)
	}
	return nil
}

func (r *OutboxRepo) Claim(ctx context.Context, batch int) ([]outbox.Claimed, error) {
	conn := r.getter.DefaultTrOrDB(ctx, r.db)

	rows, err := conn.QueryContext(ctx,
		`SELECT id, recipient, subject, body, attempts
		   FROM email_outbox
		  WHERE status = 'pending'
		    AND (next_retry_at IS NULL OR next_retry_at <= NOW(3))
		  ORDER BY id
		  LIMIT ?
		  FOR UPDATE SKIP LOCKED`, batch)
	if err != nil {
		return nil, fmt.Errorf("select claim: %w", err)
	}

	var claimed []outbox.Claimed
	for rows.Next() {
		var c outbox.Claimed
		if err := rows.Scan(&c.ID, &c.Recipient, &c.Subject, &c.Body, &c.Attempts); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("scan claim: %w", err)
		}
		claimed = append(claimed, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("rows claim: %w", err)
	}
	_ = rows.Close()

	return claimed, nil
}

func (r *OutboxRepo) Delete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	conn := r.getter.DefaultTrOrDB(ctx, r.db)
	query, args := inClause(`DELETE FROM email_outbox WHERE id IN `, ids)
	if _, err := conn.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete outbox: %w", err)
	}
	return nil
}

func (r *OutboxRepo) Reschedule(ctx context.Context, items []outbox.Retry) error {
	if len(items) == 0 {
		return nil
	}
	conn := r.getter.DefaultTrOrDB(ctx, r.db)

	var b strings.Builder
	b.WriteString(`UPDATE email_outbox SET status = 'pending', attempts = attempts + 1,
	        next_retry_at = DATE_ADD(NOW(3), INTERVAL (CASE id`)
	args := make([]any, 0, len(items)*4)
	ids := make([]int64, len(items))
	for i, it := range items {
		b.WriteString(" WHEN ? THEN ?")
		args = append(args, it.ID, it.Delay.Microseconds())
		ids[i] = it.ID
	}
	b.WriteString(` END) MICROSECOND), last_error = CASE id`)
	for _, it := range items {
		b.WriteString(" WHEN ? THEN ?")
		args = append(args, it.ID, it.LastErr[:min(len(it.LastErr), maxErrLen)])
	}

	query, idArgs := inClause(b.String()+" END WHERE id IN ", ids)
	if _, err := conn.ExecContext(ctx, query, append(args, idArgs...)...); err != nil {
		return fmt.Errorf("reschedule outbox: %w", err)
	}
	return nil
}

func (r *OutboxRepo) MarkFailed(ctx context.Context, items []outbox.Failure) error {
	if len(items) == 0 {
		return nil
	}
	conn := r.getter.DefaultTrOrDB(ctx, r.db)

	var b strings.Builder
	b.WriteString(`UPDATE email_outbox SET status = 'failed', attempts = attempts + 1,
	        last_error = CASE id`)
	args := make([]any, 0, len(items)*2)
	ids := make([]int64, len(items))
	for i, it := range items {
		b.WriteString(" WHEN ? THEN ?")
		args = append(args, it.ID, it.LastErr[:min(len(it.LastErr), maxErrLen)])
		ids[i] = it.ID
	}

	query, idArgs := inClause(b.String()+" END WHERE id IN ", ids)
	if _, err := conn.ExecContext(ctx, query, append(args, idArgs...)...); err != nil {
		return fmt.Errorf("mark failed outbox: %w", err)
	}
	return nil
}

func (r *OutboxRepo) OldestPendingAge(ctx context.Context) (time.Duration, error) {
	conn := r.getter.DefaultTrOrDB(ctx, r.db)
	var micros sql.NullInt64
	err := conn.QueryRowContext(ctx,
		`SELECT TIMESTAMPDIFF(MICROSECOND, MIN(created_at), NOW(3))
		   FROM email_outbox WHERE status = 'pending'`).Scan(&micros)
	if err != nil {
		return 0, fmt.Errorf("oldest pending age: %w", err)
	}
	if !micros.Valid {
		return 0, nil
	}
	return time.Duration(micros.Int64) * time.Microsecond, nil
}

func inClause(prefix string, ids []int64) (string, []any) {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte('(')
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('?')
		args[i] = id
	}
	b.WriteByte(')')
	return b.String(), args
}
