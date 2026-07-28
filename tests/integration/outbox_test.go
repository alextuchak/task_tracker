package integration

import (
	"context"
	"errors"
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
	"unicode/utf8"

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
	// panics stands in for a provider client blowing up mid-call
	panics atomic.Bool
	// panicOn blows up only for one recipient, the poison-message shape
	panicOn string
	// hold makes the provider slow, so the caller's context can end the call
	hold         time.Duration
	abortedByCtx bool
	// entered is closed when a call arrives, gate blocks it there until the
	// test lets go — a provider held mid-flight without guessing a duration
	entered     chan struct{}
	gate        chan struct{}
	enterOnce   sync.Once
	releaseOnce func()
}

// holdUntilReleased parks the provider inside a call until the test lets go.
// Callers must `defer release()`: cleanups run in reverse order, so the relay's
// own cleanup would wait on a parked send before any cleanup could free it —
// a failed assertion would hang the run instead of reporting.
func (s *stubSender) holdUntilReleased() {
	s.entered = make(chan struct{})
	s.gate = make(chan struct{})
	s.releaseOnce = sync.OnceFunc(func() { close(s.gate) })
	// A test that blocks on the relay before reaching its own release would
	// otherwise hang until the whole binary times out, hiding the assertion
	// it was written to make.
	time.AfterFunc(5*time.Second, s.release)
}

func (s *stubSender) release() { s.releaseOnce() }

// waitEntered fails instead of parking forever when the relay never reaches the
// provider at all — the gate is not ctx-aware, so nothing else would free it.
func (s *stubSender) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-s.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the relay never reached the provider")
	}
}

func newStubSender() *stubSender { return &stubSender{sent: map[string]int{}} }

func (s *stubSender) SendBatch(ctx context.Context, msgs []outbox.Message) ([]outbox.SendResult, error) {
	if s.panics.Load() {
		panic("provider client exploded")
	}
	for _, m := range msgs {
		if s.panicOn != "" && m.Recipient == s.panicOn {
			panic("provider client exploded on " + m.Recipient)
		}
	}
	s.mu.Lock()
	s.calls++
	s.maxBatch = max(s.maxBatch, len(msgs))
	hold, gate := s.hold, s.gate
	s.mu.Unlock()

	// the gate wait is deliberately not ctx-aware: it models a provider that
	// has already accepted the batch when cancellation arrives
	if gate != nil {
		s.enterOnce.Do(func() { close(s.entered) })
		<-gate
	}

	if hold > 0 {
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
	resetOutbox(t)
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
	return startRelayWithDedup(t, sender, cfg,
		cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler)))
}

func startRelayWithDedup(
	t *testing.T, sender outbox.Sender, cfg outbox.Config, dedup *cache.Dedup,
) relayHandle {
	t.Helper()
	repo := persistence.NewOutboxRepo(testDB, trmsql.DefaultCtxGetter)
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

func resetOutbox(t *testing.T) {
	t.Helper()
	_, err := testDB.Exec(`DELETE FROM email_outbox`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = testDB.Exec(`DELETE FROM email_outbox`) })
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
	resetOutbox(t)
	const n = 10
	marker := "claim-outbox.io"
	for i := range n {
		enqueueRow(t, fmt.Sprintf("cl%d@%s", i, marker))
	}
	repo := persistence.NewOutboxRepo(testDB, trmsql.DefaultCtxGetter)
	trManager := manager.Must(trmsql.NewDefaultFactory(testDB))

	claimedByFirst := make(chan []int64, 1)
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	t.Cleanup(releaseOnce)
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
	releaseOnce()

	require.NoError(t, err)
	require.Len(t, second, 5)
	assert.Less(t, elapsed, 2*time.Second, "the second claim must skip locked rows, not wait for them")

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
	resetOutbox(t)
	rcpt := "deliver@outbox.io"
	enqueueRow(t, rcpt)
	sender := email.NewClient(email.Config{MaxFailures: 3, OpenFor: time.Minute, Timeout: 3 * time.Second}, slog.New(slog.DiscardHandler))
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "row should be deleted after delivery")
}

func TestOutboxRejectedMessageGoesToDeadLetter(t *testing.T) {
	resetOutbox(t)
	rcpt := "rejected@outbox.io"
	id := enqueueRow(t, rcpt)
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
	// a mark here would make the next attempt skip the send and delete the row:
	// the message disappears with neither a delivery nor a dead letter
	assert.False(t, cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler)).
		WasSent(context.Background(), id))
}

// Covers the batched Reschedule: several rows, each with its own backoff and
// error text, must come back as pending in a single statement.
func TestOutboxReschedulesBatchBeforeDeadLetter(t *testing.T) {
	resetOutbox(t)
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
	resetOutbox(t)
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
	resetOutbox(t)
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
	resetOutbox(t)
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
	resetOutbox(t)
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
	resetOutbox(t)
	rcpt := "budget@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.hold = 10 * time.Second
	cfg := fastConfig(8)
	cfg.Budget = 50 * time.Millisecond
	h := startRelay(t, sender, cfg)

	require.Eventually(t, func() bool { return h.logs.has("outbox send exceeded the tick budget") },
		10*time.Second, 10*time.Millisecond, "an over-budget send must be reported as such")
	assert.False(t, h.logs.has("outbox send aborted: shutdown"))
	assert.Equal(t, "pending", outboxStatus(t, rcpt))
	assert.Zero(t, outboxAttempts(t, rcpt))
}

// The relay settles on a context that survives cancellation, so the closer has
// to wait for it — otherwise the process exits mid-settlement and the next pod
// redelivers whatever the provider already accepted.
func TestOutboxDrainWaitsForTheInFlightTickToSettle(t *testing.T) {
	resetOutbox(t)
	rcpt := "drain@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.holdUntilReleased()
	defer sender.release()
	h := startRelay(t, sender, fastConfig(8))

	sender.waitEntered(t)
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	drained := make(chan error, 1)
	go func() { drained <- h.relay.Drain(ctx) }()

	select {
	case <-drained:
		t.Fatal("Drain returned while the tick was still mid-send")
	case <-time.After(50 * time.Millisecond):
	}
	sender.release()

	require.NoError(t, <-drained)
	assert.Zero(t, outboxCount(t, rcpt), "the row must already be settled when Drain returns")
	assert.Equal(t, 1, sender.count(rcpt))
}

func TestOutboxDrainReturnsWithoutWaitingWhenIdle(t *testing.T) {
	resetOutbox(t)
	h := startRelay(t, newStubSender(), fastConfig(8))
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	require.NoError(t, h.relay.Drain(ctx))

	assert.Less(t, time.Since(start), time.Second, "an idle relay must not hold up shutdown")
}

func TestOutboxDrainReportsWhenItRunsOutOfTime(t *testing.T) {
	resetOutbox(t)
	rcpt := "drain-slow@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.holdUntilReleased()
	defer sender.release()
	h := startRelay(t, sender, fastConfig(8))

	sender.waitEntered(t)
	h.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := h.relay.Drain(ctx)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "outbox")
	assert.Equal(t, "pending", outboxStatus(t, rcpt))

	sender.release()

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		10*time.Second, 20*time.Millisecond,
		"a drain timeout must not lose the message — the tick settles regardless")
}

// last_error is sliced by bytes into a utf8mb4 column. An error message whose
// 1024th byte falls inside a character produces invalid UTF-8, MySQL rejects
// the whole settlement, and the row is re-claimed and re-failed forever.
func TestOutboxTruncatesTheErrorOnARuneBoundary(t *testing.T) {
	resetOutbox(t)
	rcpt := "runes@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	// three-byte runes stay split under any power-of-two limit, unlike two-byte
	// ones, which land on a boundary as soon as the prefix changes
	splitAtTheLimit := strings.Repeat("日", 700)
	require.False(t, utf8.ValidString(splitAtTheLimit[:1024]),
		"the fixture must actually split a character at the limit")
	sender.fail = func(r string) error {
		if r == rcpt {
			return errors.New(splitAtTheLimit)
		}
		return nil
	}
	startRelay(t, sender, fastConfig(5))

	require.Eventually(t, func() bool { return outboxAttempts(t, rcpt) >= 1 },
		5*time.Second, 20*time.Millisecond,
		"the row must be rescheduled, not wedged on an encoding error")

	assert.True(t, utf8.ValidString(storedError(t, rcpt)), "last_error must stay valid UTF-8")
	assert.True(t, strings.HasPrefix(splitAtTheLimit, storedError(t, rcpt)),
		"what is kept must be a prefix of the real error, not a placeholder")
	assert.Greater(t, len(storedError(t, rcpt)), 1000,
		"truncation must keep as much of the error as the limit allows")
}

// The dead-letter write truncates too, and it is the worse one to get wrong:
// a rejected settlement leaves the row to be re-claimed and re-failed forever.
func TestOutboxTruncatesTheErrorWhenDeadLettering(t *testing.T) {
	resetOutbox(t)
	rcpt := "runes-dead@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	splitAtTheLimit := strings.Repeat("日", 700)
	sender.fail = func(r string) error {
		if r == rcpt {
			return errors.New(splitAtTheLimit)
		}
		return nil
	}
	startRelay(t, sender, fastConfig(1))

	require.Eventually(t, func() bool { return outboxStatus(t, rcpt) == "failed" },
		5*time.Second, 20*time.Millisecond,
		"the row must reach the dead letter state, not wedge on an encoding error")

	assert.True(t, utf8.ValidString(storedError(t, rcpt)))
}

func storedError(t *testing.T, recipient string) string {
	t.Helper()
	var stored string
	require.NoError(t, testDB.QueryRow(
		`SELECT last_error FROM email_outbox WHERE recipient = ?`, recipient).Scan(&stored))
	return stored
}

// A panic in a shard goroutine reaches no caller: without a recover it takes
// the whole process down, API included, on nothing worse than a malformed
// provider response.
func TestOutboxSurvivesAPanickingProvider(t *testing.T) {
	resetOutbox(t)
	rcpt := "panic@outbox.io"
	enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.panics.Store(true)
	h := startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return h.logs.has("outbox shard panicked") },
		5*time.Second, 20*time.Millisecond, "the panic must be reported, not fatal")
	require.Eventually(t, func() bool { return outboxAttempts(t, rcpt) >= 1 },
		5*time.Second, 20*time.Millisecond,
		"the batch must spend an attempt: settling nothing re-claims it forever")

	sender.panics.Store(false)

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		20*time.Second, 50*time.Millisecond,
		"the message must survive the panic and go out once the provider recovers")
	assert.Equal(t, 1, sender.count(rcpt))
}

func TestOutboxPoisonMessageDoesNotWedgeTheQueue(t *testing.T) {
	resetOutbox(t)
	poison := "poison@outbox.io"
	behind := "behind@outbox.io"
	enqueueRow(t, poison)
	enqueueRow(t, behind)
	sender := newStubSender()
	sender.panicOn = poison
	startRelay(t, sender, fastConfig(2))

	require.Eventually(t, func() bool { return outboxCount(t, behind) == 0 },
		20*time.Second, 50*time.Millisecond,
		"a poison message must not hold the rest of the queue hostage")
	require.Eventually(t, func() bool { return outboxStatus(t, poison) == "failed" },
		20*time.Second, 50*time.Millisecond,
		"a message that keeps panicking must spend its attempts and dead-letter")
}

func TestOutboxMarksDedupEvenWhenTheBudgetIsSpent(t *testing.T) {
	resetOutbox(t)
	rcpt := "budget-mark@outbox.io"
	id := enqueueRow(t, rcpt)
	sender := newStubSender()
	sender.holdUntilReleased()
	defer sender.release()
	cfg := fastConfig(8)
	cfg.Budget = 50 * time.Millisecond
	startRelay(t, sender, cfg)

	sender.waitEntered(t)
	time.Sleep(2 * cfg.Budget)
	sender.release()

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		10*time.Second, 20*time.Millisecond)

	require.Equal(t, 1, sender.callCount())
	marked := cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler)).
		WasSent(context.Background(), id)
	assert.True(t, marked, "a delivered message must carry its dedup mark")
}

func TestOutboxDedupSuppressesResend(t *testing.T) {
	resetOutbox(t)
	rcpt := "dedup@outbox.io"
	id := enqueueRow(t, rcpt)
	cache.NewDedup(testRedis, time.Hour, slog.New(slog.DiscardHandler)).
		MarkSent(context.Background(), id)

	sender := newStubSender()
	startRelay(t, sender, fastConfig(8))

	require.Eventually(t, func() bool { return outboxCount(t, rcpt) == 0 },
		5*time.Second, 20*time.Millisecond, "already-sent row should be cleaned up")
	assert.Zero(t, sender.count(rcpt)) // dedup hit: the sender was never called
}

func TestOutboxConcurrentRelaysSendEachOnce(t *testing.T) {
	resetOutbox(t)
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

func TestUnreachableDedupDoesNotStallTheBatch(t *testing.T) {
	resetOutbox(t)
	const batch = 25
	for i := range batch {
		enqueueRow(t, fmt.Sprintf("dead-dedup-%d@outbox.io", i))
	}
	dead := cache.NewRedis(cache.Config{
		Addr: "192.0.2.1:6379", DialTimeout: time.Second, MaxRetries: 1, DialerRetries: 1,
	})
	t.Cleanup(func() { _ = dead.Close() })
	sender := newStubSender()

	startRelayWithDedup(t, sender, fastConfig(3),
		cache.NewDedup(dead, time.Hour, slog.New(slog.DiscardHandler)))

	require.Eventually(t, func() bool { return outboxTotal(t) == 0 }, 3*time.Second, 20*time.Millisecond,
		"the dedup costs a batch one dial, not %d of them", batch)
	for i := range batch {
		assert.Equal(t, 1, sender.count(fmt.Sprintf("dead-dedup-%d@outbox.io", i)),
			"delivery must not depend on the dedup being reachable")
	}
}

func outboxTotal(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, testDB.QueryRow(`SELECT COUNT(*) FROM email_outbox`).Scan(&n))
	return n
}
