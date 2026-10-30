package retention

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingSweeper struct {
	calls atomic.Int64
}

func (c *countingSweeper) Sweep(context.Context) (int64, error) {
	c.calls.Add(1)
	return 0, nil
}

func TestSchedulerSweepsOnEveryTick(t *testing.T) {
	sw := &countingSweeper{}
	s := NewScheduler(sw, time.Millisecond)

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() err = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for sw.calls.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := sw.calls.Load(); n < 3 {
		t.Fatalf("calls = %d, want at least 3", n)
	}

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() err = %v", err)
	}
	stopped := sw.calls.Load()
	time.Sleep(20 * time.Millisecond)
	if sw.calls.Load() != stopped {
		t.Error("sweep ran after Stop()")
	}
}
