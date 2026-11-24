package worker

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Locks serialises work on one key across every worker process.
type Locks interface {
	// Lock waits for the key and returns the function that releases it.
	Lock(ctx context.Context, key string) (unlock func(), err error)
}

// pgLocks are Postgres advisory locks, so they hold across processes
// without anything else to run. Each lock keeps one pooled connection
// until it is released.
type pgLocks struct {
	pool *pgxpool.Pool
}

func (l pgLocks) Lock(ctx context.Context, key string) (func(), error) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	id := int64(h.Sum64()) //nolint:gosec // any 64 bits make a lock id

	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", key, err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", id); err != nil {
		conn.Release()
		return nil, fmt.Errorf("lock %s: %w", key, err)
	}
	return func() {
		// Unlocked even when ctx is done; a connection that can't unlock
		// is closed, which drops the lock with it.
		if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", id); err != nil {
			slog.Warn("worker: unlock failed, closing the connection", "key", key, "err", err)
			_ = conn.Hijack().Close(context.Background())
			return
		}
		conn.Release()
	}, nil
}
