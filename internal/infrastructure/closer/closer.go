package closer

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	drain   string = "drain"
	release string = "release"
)

func New(log *slog.Logger, cfg Config) *Closer {
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
