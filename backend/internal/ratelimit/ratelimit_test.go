package ratelimit

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// These tests need Redis. They are skipped unless SEISMO_TEST_REDIS_URL is
// set, e.g. redis://localhost:6379/0.
func newTestLimiter(t *testing.T) *Limiter {
	t.Helper()
	url := os.Getenv("SEISMO_TEST_REDIS_URL")
	if url == "" {
		t.Skip("SEISMO_TEST_REDIS_URL not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { rdb.Close() })
	return New(rdb, fmt.Sprintf("test:ratelimit:%d:", time.Now().UnixNano()))
}

func TestBurstThenRefuse(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()
	lim := Limit{PerMinute: 10, Burst: 5}

	for i := 0; i < 5; i++ {
		res, err := l.Allow(ctx, "install-1", lim)
		if err != nil {
			t.Fatal(err)
		}
		if !res.Allowed {
			t.Fatalf("call %d refused within the burst", i+1)
		}
	}

	res, err := l.Allow(ctx, "install-1", lim)
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("call 6 allowed past the burst")
	}
	// 10 per minute: one token every 6 s.
	if res.RetryAfter <= 0 || res.RetryAfter > 6*time.Second {
		t.Errorf("RetryAfter = %v, want (0, 6s]", res.RetryAfter)
	}

	// Another key has its own bucket.
	if res, _ := l.Allow(ctx, "install-2", lim); !res.Allowed {
		t.Error("another key was refused")
	}
}

func TestRefill(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()
	// One token per 100 ms.
	lim := Limit{PerMinute: 600, Burst: 1}

	if res, _ := l.Allow(ctx, "k", lim); !res.Allowed {
		t.Fatal("first call refused")
	}
	if res, _ := l.Allow(ctx, "k", lim); res.Allowed {
		t.Fatal("second call allowed before refill")
	}
	time.Sleep(150 * time.Millisecond)
	if res, _ := l.Allow(ctx, "k", lim); !res.Allowed {
		t.Fatal("call refused after refill")
	}
}

func TestConcurrentCallsNeverOverAllow(t *testing.T) {
	l := newTestLimiter(t)
	ctx := context.Background()
	lim := Limit{PerMinute: 1, Burst: 20}

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if res, err := l.Allow(ctx, "shared", lim); err == nil && res.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != 20 {
		t.Errorf("allowed %d calls, want exactly the burst of 20", got)
	}
}

func TestInvalidLimit(t *testing.T) {
	l := New(nil, "")
	if _, err := l.Allow(context.Background(), "k", Limit{}); err == nil {
		t.Error("want an error for a zero limit")
	}
}
