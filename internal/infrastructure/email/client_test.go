package email

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"task_tracker/internal/infrastructure/outbox"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"
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

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	c := NewClient(Config{MaxFailures: 3, OpenFor: 7 * time.Second, Timeout: time.Second},
		slog.New(slog.DiscardHandler))
	var calls int
	c.send = func(context.Context, []outbox.Message) error {
		calls++
		return errors.New("provider is down")
	}
	msgs := []outbox.Message{{Recipient: "a@b.c"}}

	for range 3 {
		_, err := c.SendBatch(context.Background(), msgs)
		require.Error(t, err)
	}
	_, err := c.SendBatch(context.Background(), msgs)

	require.Equal(t, 3, calls, "an open breaker must stop calling the provider")
	var hint interface{ RetryAfterHint() time.Duration }
	require.ErrorAs(t, err, &hint, "the relay classifies the pause through this interface")
	assert.Equal(t, 7*time.Second, hint.RetryAfterHint())
	assert.ErrorIs(t, err, gobreaker.ErrOpenState, "the cause must survive the wrapping")
}

func TestBreakerClosesAgainAfterTheOpenWindow(t *testing.T) {
	c := NewClient(Config{MaxFailures: 1, OpenFor: 50 * time.Millisecond, Timeout: time.Second},
		slog.New(slog.DiscardHandler))
	down := true
	c.send = func(context.Context, []outbox.Message) error {
		if down {
			return errors.New("provider is down")
		}
		return nil
	}
	msgs := []outbox.Message{{Recipient: "a@b.c"}}
	_, err := c.SendBatch(context.Background(), msgs)
	require.Error(t, err)
	_, err = c.SendBatch(context.Background(), msgs)
	require.ErrorIs(t, err, gobreaker.ErrOpenState, "the breaker must be open before recovery means anything")

	down = false
	require.Eventually(t, func() bool {
		_, err := c.SendBatch(context.Background(), msgs)
		return err == nil
	}, 2*time.Second, 10*time.Millisecond, "the breaker must recover once the provider does")
}

func TestSendBatchBoundsTheProviderCall(t *testing.T) {
	c := NewClient(Config{MaxFailures: 3, OpenFor: 50 * time.Millisecond, Timeout: 50 * time.Millisecond},
		slog.New(slog.DiscardHandler))
	c.send = func(ctx context.Context, _ []outbox.Message) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
			return nil
		}
	}

	start := time.Now()
	_, err := c.SendBatch(context.Background(), []outbox.Message{{Recipient: "a@b.c"}})

	require.Error(t, err)
	assert.Less(t, time.Since(start), time.Second, "the cutoff must come from the client's own timeout")
	assert.NotErrorIs(t, err, context.DeadlineExceeded,
		"the relay reads a bare deadline as its own tick budget running out")
	var hint interface{ RetryAfterHint() time.Duration }
	require.ErrorAs(t, err, &hint)
	assert.Equal(t, 50*time.Millisecond, hint.RetryAfterHint())
}

// The relay cancels every sibling shard the moment one fails. Counting those
// as provider failures would let a single bad batch open the breaker on a
// four-shard tick.
func TestCallerCancellationDoesNotTripTheBreaker(t *testing.T) {
	c := NewClient(Config{MaxFailures: 2, OpenFor: 7 * time.Second, Timeout: time.Second},
		slog.New(slog.DiscardHandler))
	c.send = func(ctx context.Context, _ []outbox.Message) error {
		<-ctx.Done()
		return ctx.Err()
	}
	msgs := []outbox.Message{{Recipient: "a@b.c"}}

	for range 5 {
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(5 * time.Millisecond); cancel() }()
		_, err := c.SendBatch(ctx, msgs)
		cancel()
		require.ErrorIs(t, err, context.Canceled, "the caller's cancellation must reach the relay unchanged")
	}

	c.send = func(context.Context, []outbox.Message) error { return nil }
	_, err := c.SendBatch(context.Background(), msgs)

	require.NoError(t, err, "the breaker must still be closed")
}

// A tick fans out to several shards; in half-open gobreaker admits one and
// rejects the rest, so this is the state the relay spends most of an outage in.
func TestHalfOpenRejectionCarriesTheHint(t *testing.T) {
	c := NewClient(Config{MaxFailures: 1, OpenFor: 20 * time.Millisecond, Timeout: time.Second},
		slog.New(slog.DiscardHandler))
	probeEntered := make(chan struct{})
	releaseProbe := make(chan struct{})
	var down atomic.Bool
	down.Store(true)
	c.send = func(context.Context, []outbox.Message) error {
		if down.Load() {
			return errors.New("provider is down")
		}
		close(probeEntered)
		<-releaseProbe
		return nil
	}
	msgs := []outbox.Message{{Recipient: "a@b.c"}}
	_, err := c.SendBatch(context.Background(), msgs)
	require.Error(t, err)

	down.Store(false)
	time.Sleep(40 * time.Millisecond)
	go func() { _, _ = c.SendBatch(context.Background(), msgs) }()
	<-probeEntered
	_, err = c.SendBatch(context.Background(), msgs)
	close(releaseProbe)

	require.ErrorIs(t, err, gobreaker.ErrTooManyRequests)
	var hint interface{ RetryAfterHint() time.Duration }
	require.ErrorAs(t, err, &hint)
	assert.Equal(t, 20*time.Millisecond, hint.RetryAfterHint())
}

func TestSendBatchHonoursCancelledContext(t *testing.T) {
	c := NewClient(Config{MaxFailures: 3, OpenFor: 30 * time.Second, Timeout: 3 * time.Second}, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.SendBatch(ctx, []outbox.Message{{Recipient: "a@b.c"}})
	require.ErrorIs(t, err, context.Canceled)
}
