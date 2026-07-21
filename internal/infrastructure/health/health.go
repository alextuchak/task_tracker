package health

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

type Check func(ctx context.Context) error

var (
	ErrStarting     = errors.New("starting")
	ErrShuttingDown = errors.New("shutting down")
)

const (
	stateStarting int32 = iota
	stateReady
	stateShutdown
)

func New(cfg Config) *Health {
	return &Health{checkTimeout: cfg.CheckTimeout}
}

type Health struct {
	checks       []Check
	checkTimeout time.Duration
	state        atomic.Int32
}

func (h *Health) AddCheck(c ...Check) {
	h.checks = append(h.checks, c...)
}

func (h *Health) SetReady() {
	h.state.Store(stateReady)
}

func (h *Health) SetShuttingDown() {
	h.state.Store(stateShutdown)
}

func (h *Health) CheckReadiness(ctx context.Context) error {
	switch h.state.Load() {
	case stateStarting:
		return ErrStarting
	case stateShutdown:
		return ErrShuttingDown
	}
	ctx, cancel := context.WithTimeout(ctx, h.checkTimeout)
	defer cancel()
	for _, check := range h.checks {
		if err := check(ctx); err != nil {
			return err
		}
	}
	return nil
}
