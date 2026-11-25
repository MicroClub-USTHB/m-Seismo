package retention

import (
	"context"
	"errors"
	"testing"
)

type fakeDeleter struct {
	gotDefaultDays int32
	calls          int
	n              int64
	err            error
}

func (f *fakeDeleter) DeleteExpiredReports(_ context.Context, defaultDays int32) (int64, error) {
	f.calls++
	f.gotDefaultDays = defaultDays
	return f.n, f.err
}

func TestSweep(t *testing.T) {
	d := &fakeDeleter{n: 7}
	svc := NewService(d, 90)

	n, err := svc.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep() err = %v", err)
	}
	if n != 7 {
		t.Errorf("Sweep() = %d, want 7", n)
	}
	if d.gotDefaultDays != 90 {
		t.Errorf("default days = %d, want 90", d.gotDefaultDays)
	}
}

func TestSweepPropagatesError(t *testing.T) {
	want := errors.New("db down")
	svc := NewService(&fakeDeleter{err: want}, 90)

	if _, err := svc.Sweep(context.Background()); !errors.Is(err, want) {
		t.Errorf("Sweep() err = %v, want %v", err, want)
	}
}
