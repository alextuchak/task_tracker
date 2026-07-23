package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"task_tracker/internal/infrastructure/outbox"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
)

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
