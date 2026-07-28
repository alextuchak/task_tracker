package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultPause = 30 * time.Second
	backoffCap   = time.Hour
	settleGrace  = 5 * time.Second
	// the dedup gets one second of a tick to answer, once for the checks and
	// once for the marks: it saves duplicates, it does not get to stop delivery
	dedupGrace = time.Second
)

var errPaused = errors.New("outbox: downstream paused")

type Repo interface {
	Claim(ctx context.Context, batch int) ([]Claimed, error)
	Delete(ctx context.Context, ids []int64) error
	Reschedule(ctx context.Context, items []Retry) error
	MarkFailed(ctx context.Context, items []Failure) error
	OldestPendingAge(ctx context.Context) (time.Duration, error)
}

type Sender interface {
	SendBatch(ctx context.Context, msgs []Message) ([]SendResult, error)
}

type Deduper interface {
	WasSent(ctx context.Context, id int64) bool
	MarkSent(ctx context.Context, id int64)
}

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type retryAfter interface {
	RetryAfterHint() time.Duration
}

func (c Config) SettleWindow() time.Duration { return c.Budget + dedupGrace + settleGrace }

func NewRelay(repo Repo, sender Sender, dedup Deduper, tx TxManager, cfg Config, log *slog.Logger) *Relay {
	return &Relay{
		repo: repo, sender: sender, dedup: dedup, tx: tx,
		cfg: cfg, log: log, done: make(chan struct{}),
	}
}

type Relay struct {
	repo   Repo
	sender Sender
	dedup  Deduper
	tx     TxManager
	log    *slog.Logger
	done   chan struct{}
	cfg    Config
	paused atomic.Int64
}

func (r *Relay) Run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.cfg.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			r.tick(ctx)
		}
	}
}

func (r *Relay) Drain(ctx context.Context) error {
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("outbox relay is still settling: %w", ctx.Err())
	}
}

func (r *Relay) tick(parent context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			tickErrorsTotal.Inc()
			r.log.Error("outbox tick panicked",
				slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
			r.pause(defaultPause)
		}
	}()

	txCtx, cancelTx := context.WithTimeout(context.WithoutCancel(parent), r.cfg.SettleWindow())
	defer cancelTx()
	sendCtx, cancelSend := context.WithTimeout(parent, r.cfg.Budget)
	defer cancelSend()

	if time.Now().UnixNano() < r.paused.Load() {
		return
	}
	r.observeOldestPending(txCtx)

	var s settlement
	err := r.tx.Do(txCtx, func(ctx context.Context) error {
		claimed, err := r.repo.Claim(ctx, r.cfg.Batch)
		if err != nil {
			return err
		}
		if len(claimed) == 0 {
			return nil
		}
		s = r.dispatch(sendCtx, claimed)
		if err := r.repo.Delete(ctx, s.sent); err != nil {
			return err
		}
		if err := r.repo.Reschedule(ctx, s.reschedule); err != nil {
			return err
		}
		return r.repo.MarkFailed(ctx, s.fail)
	})
	if s.paused {
		r.pause(s.pause)
	}
	if err != nil {
		tickErrorsTotal.Inc()
		r.log.Error("outbox tick failed",
			slog.Int("delivered_before_failure", s.delivered),
			slog.Int("to_reschedule", len(s.reschedule)),
			slog.Int("to_fail", len(s.fail)),
			slog.Bool("rows_will_be_reclaimed", s.delivered > 0),
			slog.Any("error", err))
		return
	}

	sentTotal.Add(float64(s.delivered))
	failedTotal.Add(float64(len(s.fail)))
}

func (r *Relay) dispatch(ctx context.Context, claimed []Claimed) settlement {
	shards := shardBy(claimed, r.cfg.Workers)
	outcomes := make([]shardOutcome, len(shards))
	sendCtx, cancelSend := context.WithCancelCause(ctx)
	defer cancelSend(nil)

	var wg sync.WaitGroup
	for i, shard := range shards {
		wg.Go(func() { outcomes[i] = r.processShard(sendCtx, shard, cancelSend) })
	}
	wg.Wait()

	return r.collect(outcomes)
}

type shardOutcome struct {
	sent      []int64
	retry     []retryItem
	pause     time.Duration
	delivered int
	paused    bool
}

type retryItem struct {
	reason  string
	claimed Claimed
}

func (r *Relay) processShard(ctx context.Context, shard []Claimed, cancel context.CancelCauseFunc) (out shardOutcome) {
	var pending []Claimed
	defer func() {
		rec := recover()
		if rec == nil {
			return
		}
		r.log.Error("outbox shard panicked",
			slog.Int("undelivered", len(pending)-out.delivered),
			slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
		for _, c := range pending[out.delivered:] {
			out.retry = append(out.retry, retryItem{claimed: c, reason: fmt.Sprintf("relay panic: %v", rec)})
		}
		cancel(errPaused)
	}()

	if ctx.Err() != nil {
		switch {
		case errors.Is(context.Cause(ctx), errPaused):
			r.log.Debug("outbox shard skipped: downstream paused by a sibling",
				slog.Int("skipped", len(shard)))
		case errors.Is(ctx.Err(), context.Canceled):
			r.log.Info("outbox shard skipped: shutdown", slog.Int("skipped", len(shard)))
		default:
			r.log.Warn("outbox shard skipped: tick budget expired",
				slog.Int("skipped", len(shard)))
		}
		return out
	}

	pending = make([]Claimed, 0, len(shard))
	msgs := make([]Message, 0, len(shard))
	// same reason as the marks below, and a sharper one: a dedup that hangs
	// rather than refuses would otherwise spend the whole tick budget here and
	// nothing would be sent at all
	checkCtx, cancelCheck := context.WithTimeout(ctx, dedupGrace)
	defer cancelCheck()
	for _, c := range shard {
		if r.dedup.WasSent(checkCtx, c.ID) {
			out.sent = append(out.sent, c.ID)
			continue
		}
		pending = append(pending, c)
		msgs = append(msgs, c.Message)
	}
	if len(msgs) == 0 {
		return out
	}

	results, err := r.sender.SendBatch(ctx, msgs)
	if err != nil {
		var hint retryAfter
		if errors.As(err, &hint) {
			out.pause = hint.RetryAfterHint()
		}
		switch {
		case errors.Is(context.Cause(ctx), errPaused):
			sendErrorsTotal.WithLabelValues("paused").Inc()
			r.log.Debug("outbox send aborted: downstream paused by a sibling",
				slog.Int("deferred", len(msgs)))
		case errors.Is(err, context.Canceled):
			sendErrorsTotal.WithLabelValues("shutdown").Inc()
			r.log.Info("outbox send aborted: shutdown", slog.Int("deferred", len(msgs)))
			cancel(errPaused)
			return out
		case errors.Is(err, context.DeadlineExceeded):
			sendErrorsTotal.WithLabelValues("budget").Inc()
			r.log.Warn("outbox send exceeded the tick budget",
				slog.Int("deferred", len(msgs)), slog.Any("error", err))
		default:
			sendErrorsTotal.WithLabelValues("provider").Inc()
			r.log.Error("outbox batch send failed",
				slog.Int("deferred", len(msgs)),
				slog.Duration("retry_after", out.pause),
				slog.Any("error", err))
		}
		out.paused = true
		cancel(errPaused)
		return out
	}
	if len(results) != len(msgs) {
		sendErrorsTotal.WithLabelValues("mismatch").Inc()
		r.log.Error("outbox: provider returned a mismatched result count",
			slog.Int("sent", len(msgs)), slog.Int("got", len(results)))
		out.paused = true
		cancel(errPaused)
		return out
	}

	// one budget for the whole marking phase: per message it would scale with
	// shard size and could outlast the settlement it delays
	markCtx, cancelMark := context.WithTimeout(context.WithoutCancel(ctx), dedupGrace)
	defer cancelMark()
	for i, c := range pending {
		if results[i].Err != nil {
			out.retry = append(out.retry, retryItem{claimed: c, reason: results[i].Err.Error()})
			continue
		}
		// the message is already out: marking it is a post-delivery effect and
		// must not be skipped because the send used up the tick budget
		r.dedup.MarkSent(markCtx, c.ID)
		out.delivered++
		out.sent = append(out.sent, c.ID)
	}
	return out
}

type settlement struct {
	sent       []int64
	reschedule []Retry
	fail       []Failure
	pause      time.Duration
	delivered  int
	paused     bool
}

func (r *Relay) collect(outcomes []shardOutcome) settlement {
	var s settlement
	for _, o := range outcomes {
		s.sent = append(s.sent, o.sent...)
		s.delivered += o.delivered
		for _, it := range o.retry {
			attempts := it.claimed.Attempts + 1
			if attempts >= r.cfg.MaxAttempts {
				s.fail = append(s.fail, Failure{LastErr: it.reason, ID: it.claimed.ID})
				continue
			}
			s.reschedule = append(s.reschedule, Retry{
				LastErr: it.reason, Delay: backoff(attempts), ID: it.claimed.ID,
			})
		}
		if o.paused {
			s.paused = true
			s.pause = max(s.pause, o.pause)
		}
	}
	return s
}

func (r *Relay) pause(hint time.Duration) {
	d := hint
	if d <= 0 {
		d = defaultPause
	}
	r.paused.Store(time.Now().Add(d).UnixNano())
	r.log.Warn("outbox paused", slog.Duration("for", d))
}

func (r *Relay) observeOldestPending(ctx context.Context) {
	age, err := r.repo.OldestPendingAge(ctx)
	if err != nil {
		r.log.Error("outbox oldest pending", slog.Any("error", err))
		return
	}
	oldestPendingSeconds.Set(age.Seconds())
}

func shardBy(items []Claimed, workers int) [][]Claimed {
	if len(items) == 0 {
		return nil
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(items) {
		workers = len(items)
	}
	size := (len(items) + workers - 1) / workers
	shards := make([][]Claimed, 0, workers)
	for start := 0; start < len(items); start += size {
		shards = append(shards, items[start:min(start+size, len(items))])
	}
	return shards
}

func backoff(attempt int) time.Duration {
	d := time.Duration(math.Pow(float64(attempt), 4)) * time.Second
	if d > backoffCap {
		d = backoffCap
	}
	jitter := 1 + (rand.Float64()*0.2 - 0.1) // #nosec G404 -- retry jitter, not security
	return time.Duration(float64(d) * jitter)
}
