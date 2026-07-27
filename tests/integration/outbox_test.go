package integration

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
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
	// truncate returns fewer results than messages — a provider contract breach
	truncate bool
	// hold makes the provider slow, so the caller's context can end the call
	hold time.Duration
	// deaf makes the slow provider ignore cancellation, the way a client
	// that does not thread the context would
	deaf         bool
	abortedByCtx bool
}

func newStubSender() *stubSender { return &stubSender{sent: map[string]int{}} }

func (s *stubSender) SendBatch(ctx context.Context, msgs []outbox.Message) ([]outbox.SendResult, error) {
	s.mu.Lock()
	s.calls++
	s.maxBatch = max(s.maxBatch, len(msgs))
	hold, deaf := s.hold, s.deaf
	s.mu.Unlock()

	switch {
	case hold > 0 && deaf:
		time.Sleep(hold)
	case hold > 0:
		select {
		case <-ctx.Done():
			s.mu.Lock()
			s.abortedByCtx = true
			s.mu.Unlock()
			return nil, ctx.Err()
		case <-time.After(hold):
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failBatch != nil {
		if err := s.failBatch(msgs); err != nil {
			return nil, err
		}
	}
	if s.truncate {
		return make([]outbox.SendResult, len(msgs)-1), nil
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

func (s *stubSender) aborted() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.abortedByCtx
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

type logCapture struct {
	mu       sync.Mutex
	messages []string
}

func (c *logCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *logCapture) WithAttrs([]slog.Attr) slog.Handler       { return c }
func (c *logCapture) WithGroup(string) slog.Handler            { return c }

func (c *logCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, r.Message)
	return nil
}

func (c *logCapture) has(message string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Contains(c.messages, message)
}

func fastConfig(maxAttempts int) outbox.Config {
	return outbox.Config{
		Tick: 10 * time.Millisecond, Budget: time.Minute,
		Batch: 100, Workers: 4, MaxAttempts: maxAttempts,
	}
}

type relayHandle struct {
	relay  *outbox.Relay
	logs   *logCapture
	cancel context.CancelFunc
}

func startRelay(t *testing.T, sender outbox.Sender, cfg outbox.Config) relayHandle {
	t.Helper()
	repo := persistence.NewOutboxRepo(testDB, trmsql.DefaultCtxGetter)
	dedup := cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler))
	trManager := manager.Must(trmsql.NewDefaultFactory(testDB))
	logs := &logCapture{}
	relay := outbox.NewRelay(repo, sender, dedup, trManager, cfg, slog.New(logs))

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { relay.Run(ctx) })
	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})
	return relayHandle{relay: relay, logs: logs, cancel: cancel}
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

func outboxAttempts(t *testing.T, recipient string) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(
		`SELECT attempts FROM email_outbox WHERE recipient = ?`, recipient).Scan(&n))
	return n
}

// Exactly-once delivery survives without SKIP LOCKED — the second relay would
// merely block until the first commits. What SKIP LOCKED buys is that it does
// not block, so every replica does work instead of waiting on row locks.
func TestOutboxClaimSkipsRowsLockedByAnotherTransaction(t *testing.T) {
	const n = 10
	marker := "claim-outbox.io"
	for i := range n {
		enqueueRow(t, fmt.Sprintf("cl%d@%s", i, marker))
	}
	repo := persistence.NewOutboxRepo(testDB, trmsql.DefaultCtxGetter)
	trManager := manager.Must(trmsql.NewDefaultFactory(testDB))

	claimedByFirst := make(chan []int64, 1)
	release := make(chan struct{})
	go func() {
		_ = trManager.Do(context.Background(), func(ctx context.Context) error {
			claimed, err := repo.Claim(ctx, 5)
			if err != nil {
				close(claimedByFirst)
				return err
			}
			ids := make([]int64, 0, len(claimed))
			for _, c := range claimed {
				ids = append(ids, c.ID)
			}
			claimedByFirst <- ids
			<-release
			return nil
		})
	}()

	first := <-claimedByFirst
	require.Len(t, first, 5)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var second []int64
	start := time.Now()
	err := trManager.Do(ctx, func(ctx context.Context) error {
		claimed, err := repo.Claim(ctx, 5)
		for _, c := range claimed {
			second = append(second, c.ID)
		}
		return err
	})
	elapsed := time.Since(start)
	close(release)

	require.NoError(t, err)
	require.Len(t, second, 5)
	assert.Less(t, elapsed, time.Second, "the second claim must skip locked rows, not wait for them")

	held := make(map[int64]bool, len(first))
	for _, id := range first {
		held[id] = true
	}
	for _, id := range second {
		assert.False(t, held[id], "both transactions claimed row %d", id)
	}
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
	var down atomic.Bool
	down.Store(true)
	sender := newStubSender()
	sender.failBatch = func(msgs []outbox.Message) error {
		for _, m := range msgs {
			if m.Recipient == rcpt && down.Load() {
				calls.Add(1)
				return stubErr{retryAfter: 20 * time.Millisecond}
			}
		}
		return nil
	}
	startRelay(t, sender, fastConfig(3))

	require.Eventually(t, func() bool { return calls.Load() >= 2 },
		5*time.Second, 20*time.Millisecond, "downstream should be retried while it is down")
	require.Equal(t, "pending", outboxStatus(t, rcpt))
	require.Zero(t, outboxAttempts(t, rcpt),
		"a downstream outage must not spend the message's own attempts")

	down.Store(false)

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "message should be delivered once downstream recovers")
	assert.Equal(t, 1, sender.count(rcpt))
}

func TestOutboxHonoursRetryAfterBeforeCallingAgain(t *testing.T) {
	rcpt := "backoff@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.failBatch = func([]outbox.Message) error {
		return stubErr{retryAfter: 2 * time.Second}
	}
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return sender.callCount() >= 1 },
		5*time.Second, 10*time.Millisecond)
	calls := sender.callCount()
	time.Sleep(300 * time.Millisecond) // ~30 ticks the relay must sit out

	assert.Equal(t, calls, sender.callCount(),
		"the relay must sit out the provider's retry-after instead of ticking into it")
	assert.Equal(t, "pending", outboxStatus(t, rcpt))
	assert.Zero(t, outboxAttempts(t, rcpt))
}

// A provider that returns fewer results than messages must not be indexed
// against — the guard turns it into a pause, and the relay survives to deliver
// once the provider behaves.
func TestOutboxSurvivesMismatchedResultCount(t *testing.T) {
	rcpt := "mismatch@outbox.io"
	enqueueRow(t, rcpt)
	broken := newStubSender()
	broken.truncate = true
	startRelay(t, broken, fastConfig(8))

	require.Eventually(t, func() bool { return broken.callCount() >= 1 },
		5*time.Second, 10*time.Millisecond)
	require.Equal(t, "pending", outboxStatus(t, rcpt))
	require.Zero(t, outboxAttempts(t, rcpt))

	healthy := newStubSender()
	startRelay(t, healthy, fastConfig(8))

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "the message must survive the breach and still be deliverable")
	assert.Equal(t, 1, healthy.count(rcpt))
}

// A shutdown must abort the in-flight send, and must not be reported as the
// tick running out of budget — the two call for opposite operator reactions.
func TestOutboxAbortsSendOnShutdown(t *testing.T) {
	rcpt := "shutdown@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.hold = 10 * time.Second
	h := startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return sender.callCount() >= 1 },
		5*time.Second, 10*time.Millisecond)
	h.cancel()

	require.Eventually(t, func() bool { return sender.aborted() },
		5*time.Second, 10*time.Millisecond, "shutdown must end the in-flight send")
	require.Eventually(t, func() bool { return h.logs.has("outbox send aborted: shutdown") },
		5*time.Second, 10*time.Millisecond)
	assert.False(t, h.logs.has("outbox send exceeded the tick budget"))
	assert.Equal(t, "pending", outboxStatus(t, rcpt))
	assert.Zero(t, outboxAttempts(t, rcpt))
}

func TestOutboxAbandonsSendWhenTickBudgetExpires(t *testing.T) {
	rcpt := "budget@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.hold = 10 * time.Second
	cfg := fastConfig(8)
	cfg.Budget = 50 * time.Millisecond
	h := startRelay(t, sender, cfg)

	require.Eventually(t, func() bool { return h.logs.has("outbox send exceeded the tick budget") },
		5*time.Second, 10*time.Millisecond, "an over-budget send must be reported as such")
	assert.False(t, h.logs.has("outbox send aborted: shutdown"))
	assert.Equal(t, "pending", outboxStatus(t, rcpt))
	assert.Zero(t, outboxAttempts(t, rcpt))
}

// The relay settles on a context that survives cancellation, so the closer has
// to wait for it — otherwise the process exits mid-settlement and the next pod
// redelivers whatever the provider already accepted.
func TestOutboxDrainWaitsForTheInFlightTickToSettle(t *testing.T) {
	rcpt := "drain@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.hold = 300 * time.Millisecond
	sender.deaf = true
	h := startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return sender.callCount() >= 1 },
		5*time.Second, 10*time.Millisecond)
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, h.relay.Drain(ctx))

	assert.Zero(t, outboxCount(t, rcpt), "the row must already be settled when Drain returns")
	assert.Equal(t, 1, sender.count(rcpt))
}

func TestOutboxDrainReturnsWithoutWaitingWhenIdle(t *testing.T) {
	h := startRelay(t, newStubSender(), fastConfig(8))
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, h.relay.Drain(ctx))

	assert.Less(t, time.Since(start), time.Second, "an idle relay must not hold up shutdown")
}

func TestOutboxDrainReportsWhenItRunsOutOfTime(t *testing.T) {
	rcpt := "drain-slow@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.hold = 2 * time.Second
	sender.deaf = true
	h := startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return sender.callCount() >= 1 },
		5*time.Second, 10*time.Millisecond)
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	require.ErrorIs(t, h.relay.Drain(ctx), context.DeadlineExceeded)
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
