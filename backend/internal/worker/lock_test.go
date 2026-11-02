package worker

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/testenv"
)

func TestPGLocks(t *testing.T) {
	url := testenv.DatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	locks := pgLocks{pool: pool}

	unlock, err := locks.Lock(ctx, "repo/fp")
	if err != nil {
		t.Fatal(err)
	}

	// Another key is free.
	other, err := locks.Lock(ctx, "repo/other")
	if err != nil {
		t.Fatal(err)
	}
	other()

	// The same key waits for the first holder.
	got := make(chan func())
	go func() {
		u, err := locks.Lock(ctx, "repo/fp")
		if err != nil {
			t.Error(err)
			close(got)
			return
		}
		got <- u
	}()
	select {
	case <-got:
		t.Fatal("the same key was locked twice")
	case <-time.After(200 * time.Millisecond):
	}
	unlock()
	select {
	case u := <-got:
		if u != nil {
			u()
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lock was not handed over once released")
	}

	// A wait gives up with its context.
	unlock, err = locks.Lock(ctx, "repo/fp")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	short, stop := context.WithTimeout(ctx, 100*time.Millisecond)
	defer stop()
	if _, err := locks.Lock(short, "repo/fp"); err == nil {
		t.Error("waiting for a held lock ignored the context")
	}
}
