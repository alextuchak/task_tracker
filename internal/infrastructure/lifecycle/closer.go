// Package lifecycle owns app startup and shutdown: the Starter pings
// registered connections before serving, the Closer drains and releases
// them gracefully on the way out.
package lifecycle

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
)

const (
	drain   string = "drain"
	release string = "release"
)

type CloserConfig struct {
	Total time.Duration `yaml:"total" env-default:"20s"`
	Phase time.Duration `yaml:"phase" env-default:"10s"`
}

func (c *CloserConfig) Validate() error {
	if c.Total <= 0 || c.Phase <= 0 {
		return fmt.Errorf("timeouts must be positive, got total: %s, phase: %s", c.Total, c.Phase)
	}
	// drain and release draw on the same total in turn, so a phase worth more
	// than half of it can leave the other with an expired context
	// halving rather than doubling: 2*Phase overflows int64 at durations
	// time.ParseDuration will happily accept
	if c.Phase > c.Total/2 {
		return fmt.Errorf("phase timeout %s leaves no room for the second phase within total %s",
			c.Phase, c.Total)
	}
	return nil
}

func NewCloser(log *slog.Logger, cfg CloserConfig) *Closer {
	return &Closer{log: log, shutdownTimeout: cfg.Total, phaseTimeout: cfg.Phase}
}

type Closer struct {
	log             *slog.Logger
	close           []func(context.Context) error
	drain           []func(context.Context) error
	shutdownTimeout time.Duration
	phaseTimeout    time.Duration
	once            sync.Once
	mu              sync.Mutex
}

func (c *Closer) AddClose(f ...func(context.Context) error) {
	c.mu.Lock()
	c.close = append(c.close, f...)
	c.mu.Unlock()
}

func (c *Closer) AddDrain(f ...func(context.Context) error) {
	c.mu.Lock()
	c.drain = append(c.drain, f...)
	c.mu.Unlock()
}

func (c *Closer) ShutDown() {
	ctx, stop := context.WithTimeout(context.Background(), c.shutdownTimeout)
	defer stop()
	c.once.Do(func() {
		c.log.Info("shutdown started")
		c.mu.Lock()
		dr, cl := c.drain, c.close
		c.drain, c.close = nil, nil
		c.mu.Unlock()
		c.runPhase(ctx, drain, dr)
		c.runPhase(ctx, release, cl)
	})
}

func (c *Closer) runPhase(ctx context.Context, name string, f []func(context.Context) error) {
	if len(f) == 0 {
		return
	}
	c.log.Info("shutdown phase started", slog.String("phase", name))
	errs := make(chan error, len(f))

	ctx, cancel := context.WithTimeout(ctx, c.phaseTimeout)
	defer cancel()

	for _, f := range f {
		go func(f func(context.Context) error) {
			// closers run last, with nothing above them to recover: a panic
			// here would abort the shutdown instead of finishing it
			defer func() {
				if rec := recover(); rec != nil {
					c.log.Error("closer panicked", slog.String("phase", name),
						slog.Any("panic", rec), slog.String("stack", string(debug.Stack())))
					// the phase counts answers, and this closer owes one; the
					// failure itself is already in the line above
					errs <- nil
				}
			}()
			errs <- f(ctx)
		}(f)
	}

	for i := 0; i < cap(errs); i++ {
		select {
		case err := <-errs:
			if err != nil {
				c.log.Error("error returned from closer func", slog.Any("err", err))
			}
		case <-ctx.Done():
			c.log.Warn("phase deadline exceeded, abandoning remaining closers",
				slog.String("phase", name),
				slog.Int("pending", cap(errs)-i))
			return
		}
	}
}
