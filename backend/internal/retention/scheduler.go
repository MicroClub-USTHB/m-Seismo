package retention

import (
	"context"
	"log/slog"
	"time"
)

// SweepInterval is how often the scheduler runs the sweep in production.
const SweepInterval = 24 * time.Hour

// Sweeper runs one retention sweep. Implemented by *Service.
type Sweeper interface {
	Sweep(ctx context.Context) (int64, error)
}

// Scheduler runs a Sweeper on a fixed interval, until Stop. Each sweep
// outlives the request that started the scheduler, so it runs on its own
// background context rather than one tied to Start's caller.
type Scheduler struct {
	sweep    Sweeper
	interval time.Duration

	cancel context.CancelFunc
	done   chan struct{}
}

func NewScheduler(sweep Sweeper, interval time.Duration) *Scheduler {
	return &Scheduler{sweep: sweep, interval: interval}
}

func (s *Scheduler) Start(context.Context) error {
	base, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go s.run(base)
	return nil
}

func (s *Scheduler) Stop(ctx context.Context) error {
	if s.cancel == nil {
		return nil
	}
	s.cancel()
	select {
	case <-s.done:
	case <-ctx.Done():
	}
	return nil
}

func (s *Scheduler) run(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := s.sweep.Sweep(ctx); err != nil {
				slog.Error("retention: sweep failed", "err", err)
			}
		}
	}
}
