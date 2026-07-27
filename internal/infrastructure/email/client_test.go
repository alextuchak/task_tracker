package email

import (
	"context"
	"log/slog"
	"task_tracker/internal/infrastructure/outbox"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSendBatchDelivers(t *testing.T) {
	c := NewClient(Config{MaxFailures: 3, OpenFor: 30 * time.Second, Timeout: 3 * time.Second}, slog.New(slog.DiscardHandler))

	msgs := []outbox.Message{
		{Recipient: "a@b.c", Subject: "subject", Body: "body"},
		{Recipient: "d@e.f", Subject: "subject", Body: "body"},
	}
	results, err := c.SendBatch(context.Background(), msgs)
	require.NoError(t, err)
	require.Len(t, results, len(msgs), "one verdict per message, positional")
	for i, r := range results {
		require.NoError(t, r.Err, "message %d should be delivered", i)
	}
}

func TestSendBatchHonoursCancelledContext(t *testing.T) {
	c := NewClient(Config{MaxFailures: 3, OpenFor: 30 * time.Second, Timeout: 3 * time.Second}, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.SendBatch(ctx, []outbox.Message{{Recipient: "a@b.c"}})
	require.ErrorIs(t, err, context.Canceled)
}
