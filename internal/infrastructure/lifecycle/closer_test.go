package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNothingIsReleasedUntilTheDrainIsDone(t *testing.T) {
	var mu sync.Mutex
	var order []string
	record := func(name string) func(context.Context) error {
		return func(context.Context) error {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			return nil
		}
	}
	c := NewCloser(discard(), CloserConfig{Total: time.Second, Phase: 500 * time.Millisecond})
	c.AddClose(record("release"))
	c.AddDrain(func(ctx context.Context) error {
		time.Sleep(20 * time.Millisecond)
		return record("drain")(ctx)
	})

	c.ShutDown()

	assert.Equal(t, []string{"drain", "release"}, order)
}

func TestAStuckDrainStillGivesUpItsConnections(t *testing.T) {
	var released atomic.Bool
	c := NewCloser(discard(), CloserConfig{Total: time.Second, Phase: 50 * time.Millisecond})
	c.AddDrain(func(context.Context) error {
		time.Sleep(2 * time.Second)
		return nil
	})
	c.AddClose(func(context.Context) error { released.Store(true); return nil })

	done := make(chan struct{})
	go func() { c.ShutDown(); close(done) }()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("shutdown hung on a drain that never returns")
	}
	assert.True(t, released.Load(), "the release phase must run on its own budget")
}

func TestAPanickingCloserDoesNotStopTheRest(t *testing.T) {
	var released atomic.Int32
	var logs bytes.Buffer
	c := NewCloser(slog.New(slog.NewJSONHandler(&logs, nil)),
		CloserConfig{Total: time.Second, Phase: 200 * time.Millisecond})
	c.AddClose(
		func(context.Context) error { panic("driver: close of closed connection") },
		func(context.Context) error { released.Add(1); return nil },
	)

	started := time.Now()
	require.NotPanics(t, c.ShutDown)

	assert.EqualValues(t, 1, released.Load(), "the other connections still have to be let go")
	assert.Contains(t, logs.String(), "close of closed connection",
		"a swallowed panic leaves a connection that was never released looking clean")
	assert.Less(t, time.Since(started), 100*time.Millisecond,
		"a panic must report itself, not be waited out for the whole phase")
}

func TestAFailingCloserDoesNotStopTheRest(t *testing.T) {
	var released atomic.Int32
	c := NewCloser(discard(), CloserConfig{Total: time.Second, Phase: 200 * time.Millisecond})
	c.AddClose(
		func(context.Context) error { return errors.New("mysql: already closed") },
		func(context.Context) error { released.Add(1); return nil },
	)

	c.ShutDown()

	assert.EqualValues(t, 1, released.Load())
}

func TestShutDownIsIdempotent(t *testing.T) {
	var released atomic.Int32
	c := NewCloser(discard(), CloserConfig{Total: time.Second, Phase: 200 * time.Millisecond})
	c.AddClose(func(context.Context) error { released.Add(1); return nil })

	c.ShutDown()
	c.ShutDown()

	assert.EqualValues(t, 1, released.Load(), "a second signal must not close anything twice")
}

func TestAPhaseCannotBeGivenTheWholeBudget(t *testing.T) {
	require.Error(t, (&CloserConfig{Total: 30 * time.Second, Phase: 30 * time.Second}).Validate())
	require.Error(t, (&CloserConfig{Total: 30 * time.Second, Phase: 16 * time.Second}).Validate())
	require.NoError(t, (&CloserConfig{Total: 30 * time.Second, Phase: 15 * time.Second}).Validate())
	require.Error(t, (&CloserConfig{Total: 30 * time.Second}).Validate(),
		"a phase worth nothing abandons every closer the moment it starts")
	require.Error(t, (&CloserConfig{Total: 30 * time.Second, Phase: 2000000 * time.Hour}).Validate(),
		"ParseDuration accepts this, and doubling it wraps past zero")
}

func TestTheTotalBindsWhateverThePhaseSays(t *testing.T) {
	const total, phase = 150 * time.Millisecond, 100 * time.Millisecond
	require.Error(t, (&CloserConfig{Total: total, Phase: phase}).Validate(),
		"this config is the one Validate exists to reject")

	var left time.Duration
	c := NewCloser(discard(), CloserConfig{Total: total, Phase: phase})
	c.AddDrain(func(ctx context.Context) error { <-ctx.Done(); return nil })
	c.AddClose(func(ctx context.Context) error {
		deadline, _ := ctx.Deadline()
		left = time.Until(deadline)
		return nil
	})

	c.ShutDown()

	assert.InDelta(t, float64(total-phase), float64(left), float64(30*time.Millisecond),
		"the release phase may only have what the total has left, not a fresh phase")
}
