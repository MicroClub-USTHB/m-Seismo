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

// Scheduler runs a Sweeper on a fixed interval, until Stop.
type Scheduler struct {
	sweep    Sweeper
	interval time.Duration

	stop chan struct{}
	done chan struct{}
}

func NewScheduler(sweep Sweeper, interval time.Duration) *Scheduler {
	return &Scheduler{sweep: sweep, interval: interval}
}

func (s *Scheduler) Start(context.Context) error {
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.run()
	return nil
}

func (s *Scheduler) Stop(context.Context) error {
	if s.stop == nil {
		return nil
	}
	close(s.stop)
	<-s.done
	return nil
}

func (s *Scheduler) run() {
	defer close(s.done)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			if _, err := s.sweep.Sweep(context.Background()); err != nil {
				slog.Error("retention: sweep failed", "err", err)
			}
		}
	}
}
