package starter

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type Ping func(ctx context.Context) error

func New(log *slog.Logger, cfg Config) *Starter {
	return &Starter{log: log, timeout: cfg.Timeout}
}

type Starter struct {
	log     *slog.Logger
	pings   []Ping
	timeout time.Duration
	mu      sync.Mutex
}

func (s *Starter) AddPing(p ...Ping) {
	s.mu.Lock()
	s.pings = append(s.pings, p...)
	s.mu.Unlock()
}

func (s *Starter) Start(ctx context.Context) error {
	s.mu.Lock()
	pings := s.pings
	s.mu.Unlock()
	if len(pings) == 0 {
		return nil
	}
	s.log.Info("startup pings started", slog.Int("count", len(pings)))

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	errs := make([]error, len(pings))
	var wg sync.WaitGroup
	for i, p := range pings {
		wg.Add(1)
		go func(i int, p Ping) {
			defer wg.Done()
			errs[i] = p(ctx)
		}(i, p)
	}
	wg.Wait()
	return errors.Join(errs...)
}
