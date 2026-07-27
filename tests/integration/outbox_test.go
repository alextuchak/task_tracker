package integration

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"task_tracker/internal/infrastructure/cache"
	"task_tracker/internal/infrastructure/email"
	"task_tracker/internal/infrastructure/outbox"
	"task_tracker/internal/infrastructure/persistence"
	"testing"
	"time"

	trmsql "github.com/avito-tech/go-transaction-manager/drivers/sql/v2"
	"github.com/avito-tech/go-transaction-manager/trm/v2/manager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSender doubles the unmanaged email provider so the relay's failure
// handling can be driven and its sends counted. Failure simulation lives here,
// in the test — not in the production sender.
type stubSender struct {
	mu       sync.Mutex
	sent     map[string]int
	calls    int
	maxBatch int
	// fail rejects individual recipients — the message's own problem
	fail func(recipient string) error
	// failBatch fails the whole provider call — the downstream's problem
	failBatch func(msgs []outbox.Message) error
}

func newStubSender() *stubSender { return &stubSender{sent: map[string]int{}} }

func (s *stubSender) SendBatch(_ context.Context, msgs []outbox.Message) ([]outbox.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.maxBatch = max(s.maxBatch, len(msgs))
	if s.failBatch != nil {
		if err := s.failBatch(msgs); err != nil {
			return nil, err
		}
	}
	results := make([]outbox.SendResult, len(msgs))
	for i, m := range msgs {
		if s.fail != nil {
			if err := s.fail(m.Recipient); err != nil {
				results[i].Err = err
				continue
			}
		}
		s.sent[m.Recipient]++
	}
	return results, nil
}

func (s *stubSender) count(recipient string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent[recipient]
}

func (s *stubSender) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubSender) largestBatch() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxBatch
}

// The whole point of SendBatch: a shard costs one provider call, not one per
// message. A per-message sender would hammer the provider's rate limit — and
// with four workers, four times faster.
func TestOutboxSendsOneCallPerShard(t *testing.T) {
	const n = 40
	marker := "batched-outbox.io"
	for i := range n {
		enqueueRow(t, fmt.Sprintf("b%d@%s", i, marker))
	}
	sender := newStubSender()
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool {
		var left int
		require.NoError(t, testDB.QueryRow(
			`SELECT COUNT(*) FROM email_outbox WHERE recipient LIKE ?`, "%"+marker).Scan(&left))
		return left == 0
	}, 10*time.Second, 25*time.Millisecond)

	assert.Greater(t, sender.largestBatch(), 1, "messages must be grouped into one call")
	assert.Less(t, sender.callCount(), n, "a per-message sender would make n calls")
	for i := range n {
		assert.Equal(t, 1, sender.count(fmt.Sprintf("b%d@%s", i, marker)))
	}
}

// stubErr carries the relay's only classification hint: how long a failed
// provider call wants to be left alone.
type stubErr struct {
	retryAfter time.Duration
}

func (e stubErr) Error() string                 { return "stub send error" }
func (e stubErr) RetryAfterHint() time.Duration { return e.retryAfter }

func fastConfig(maxAttempts int) outbox.Config {
	return outbox.Config{
		Tick: 10 * time.Millisecond, Budget: time.Minute,
		Batch: 100, Workers: 4, MaxAttempts: maxAttempts,
	}
}

func startRelay(t *testing.T, sender outbox.Sender, cfg outbox.Config) {
	t.Helper()
	repo := persistence.NewOutboxRepo(testDB, trmsql.DefaultCtxGetter)
	dedup := cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler))
	trManager := manager.Must(trmsql.NewDefaultFactory(testDB))
	relay := outbox.NewRelay(repo, sender, dedup, trManager, cfg, slog.New(slog.DiscardHandler))

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { relay.Run(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
}

func enqueueRow(t *testing.T, recipient string) int64 {
	t.Helper()
	res, err := testDB.Exec(
		`INSERT INTO email_outbox (recipient, subject, body) VALUES (?, 'subj', 'body')`, recipient)
	require.NoError(t, err)
	id, err := res.LastInsertId()
	require.NoError(t, err)
	return id
}

func outboxCount(t *testing.T, recipient string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT COUNT(*) FROM email_outbox WHERE recipient = ?`, recipient).Scan(&n))
	return n
}

func outboxStatus(t *testing.T, recipient string) string {
	t.Helper()
	var s string
	require.NoError(t, testDB.QueryRow(
		`SELECT status FROM email_outbox WHERE recipient = ?`, recipient).Scan(&s))
	return s
}

// TestOutboxDelivers drives the real (mock) email.Client end to end.
func TestOutboxDelivers(t *testing.T) {
	rcpt := "deliver@outbox.io"
	enqueueRow(t, rcpt)
	sender := email.NewClient(email.Config{MaxFailures: 3, OpenFor: time.Minute, Timeout: 3 * time.Second}, slog.New(slog.DiscardHandler))
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "row should be deleted after delivery")
}

func TestOutboxRejectedMessageGoesToDeadLetter(t *testing.T) {
	rcpt := "rejected@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.fail = func(r string) error {
		if r == rcpt {
			return stubErr{}
		}
		return nil
	}
	startRelay(t, sender, fastConfig(1)) // one attempt, then dead-letter

	require.Eventually(t, func() bool { return outboxStatus(t, rcpt) == "failed" },
		5*time.Second, 20*time.Millisecond, "a rejected recipient should dead-letter the row")
	assert.Zero(t, sender.count(rcpt))
}

// Covers the batched Reschedule: several rows, each with its own backoff and
// error text, must come back as pending in a single statement.
func TestOutboxReschedulesBatchBeforeDeadLetter(t *testing.T) {
	const n = 3
	marker := "reschedule-outbox.io"
	for i := range n {
		enqueueRow(t, fmt.Sprintf("r%d@%s", i, marker))
	}
	sender := newStubSender()
	sender.fail = func(r string) error {
		if strings.HasSuffix(r, marker) {
			return stubErr{}
		}
		return nil
	}
	startRelay(t, sender, fastConfig(5)) // room to retry before dead-lettering

	require.Eventually(t, func() bool {
		var rescheduled int
		require.NoError(t, testDB.QueryRow(
			`SELECT COUNT(*) FROM email_outbox
			  WHERE recipient LIKE ? AND status = 'pending'
			    AND attempts >= 1 AND next_retry_at IS NOT NULL`, "%"+marker).Scan(&rescheduled))
		return rescheduled == n
	}, 5*time.Second, 20*time.Millisecond, "all rows should be rescheduled with a backoff")
}

func TestOutboxRetriesTransientWithoutSpendingAttempts(t *testing.T) {
	rcpt := "transient@outbox.io"
	enqueueRow(t, rcpt)
	var calls atomic.Int32
	sender := newStubSender()
	sender.failBatch = func(msgs []outbox.Message) error {
		for _, m := range msgs {
			// downstream is down for the first two ticks that carry this message
			if m.Recipient == rcpt && calls.Add(1) <= 2 {
				return stubErr{retryAfter: 20 * time.Millisecond}
			}
		}
		return nil
	}
	startRelay(t, sender, fastConfig(3))

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "message should be delivered once downstream recovers")
	assert.Equal(t, 1, sender.count(rcpt))
}

func TestOutboxDedupSuppressesResend(t *testing.T) {
	rcpt := "dedup@outbox.io"
	id := enqueueRow(t, rcpt)
	// a prior run already delivered this message and left the marker in Redis
	cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler)).
		MarkSent(context.Background(), id)

	sender := newStubSender()
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "already-sent row should be cleaned up")
	assert.Zero(t, sender.count(rcpt)) // dedup hit: the sender was never called
}

func TestOutboxConcurrentRelaysSendEachOnce(t *testing.T) {
	const n = 20
	marker := "concurrent-outbox.io"
	for i := range n {
		enqueueRow(t, fmt.Sprintf("c%d@%s", i, marker))
	}
	sender := newStubSender()
	// two relays polling the same table: SKIP LOCKED must keep them disjoint
	startRelay(t, sender, fastConfig(8))
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool {
		var n int
		require.NoError(t, testDB.QueryRow(
			`SELECT COUNT(*) FROM email_outbox WHERE recipient LIKE ?`, "%"+marker).Scan(&n))
		return n == 0
	}, 10*time.Second, 25*time.Millisecond)

	for i := range n {
		assert.Equal(t, 1, sender.count(fmt.Sprintf("c%d@%s", i, marker)))
	}
}
