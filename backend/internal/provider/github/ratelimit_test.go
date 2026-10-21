package github

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

func TestSecondaryRateLimitIsRetried(t *testing.T) {
	p, fake := newTestProvider(t)
	op, err := p.Opener(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	ref, err := op.Open(context.Background(), issue.Issue{Repository: "octo-org/slow", Title: "x"})
	if err != nil {
		t.Fatalf("Open after a secondary limit: %v", err)
	}
	if ref.ID != "octo-org/slow#1" {
		t.Errorf("ref = %+v", ref)
	}
	if fake.slowAttempts != 2 {
		t.Errorf("attempts = %d, want 2 (one limited, one retried)", fake.slowAttempts)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("retried after %s, want >= Retry-After (1s)", elapsed)
	}
}

func TestSecondaryRateLimitBeyondDeadlineFailsFast(t *testing.T) {
	p, fake := newTestProvider(t)
	op, _ := p.Opener(context.Background(), "42")

	// Retry-After is 1s but only 500ms are left: no point waiting.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := op.Open(ctx, issue.Issue{Repository: "octo-org/slow", Title: "x"})

	var rl *issue.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *issue.RateLimitError", err)
	}
	if !rl.Secondary || rl.Wait != time.Second || rl.RetryAfter() != time.Second {
		t.Errorf("rate limit error = %+v", rl)
	}
	if fake.slowAttempts != 1 {
		t.Errorf("attempts = %d, want 1", fake.slowAttempts)
	}
}

func TestPrimaryRateLimitFailsFast(t *testing.T) {
	p, _ := newTestProvider(t)
	op, _ := p.Opener(context.Background(), "42")

	start := time.Now()
	_, err := op.Open(context.Background(), issue.Issue{Repository: "octo-org/exhausted", Title: "x"})
	var rl *issue.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *issue.RateLimitError", err)
	}
	if rl.Secondary {
		t.Error("want a primary rate limit")
	}
	if rl.Wait < 59*time.Minute || rl.Wait > 61*time.Minute {
		t.Errorf("wait = %s, want about an hour (until X-RateLimit-Reset)", rl.Wait)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %s; a reset an hour away must not be waited for", elapsed)
	}

	// go-github now knows the quota is gone and must not call GitHub again
	// for this installation; we still get a typed rate-limit error.
	_, err = op.Open(context.Background(), issue.Issue{Repository: "octo-org/app", Title: "y"})
	if !errors.As(err, &rl) {
		t.Fatalf("second call: err = %v, want *issue.RateLimitError", err)
	}
}

func TestWritesArePaced(t *testing.T) {
	p, _ := newTestProvider(t, func(o *Options) { o.MinWriteInterval = 300 * time.Millisecond })
	op, _ := p.Opener(context.Background(), "42")
	ref := issue.Ref{ID: "octo-org/app#7"}

	start := time.Now()
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := op.Comment(context.Background(), ref, "hi"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	// Three concurrent writes: slots at 0, 300ms, 600ms.
	if elapsed := time.Since(start); elapsed < 600*time.Millisecond {
		t.Errorf("3 writes took %s, want >= 600ms with a 300ms interval", elapsed)
	}

	// Reads are not paced.
	start = time.Now()
	for range 3 {
		if _, err := op.IsOpen(context.Background(), ref); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Errorf("3 reads took %s; reads must not be paced", elapsed)
	}
}

func TestMilestonesAreCached(t *testing.T) {
	p, fake := newTestProvider(t)
	op, _ := p.Opener(context.Background(), "42")

	for range 3 {
		if _, err := op.Open(context.Background(), issue.Issue{Repository: "octo-org/app", Title: "x", Milestone: "v1.0"}); err != nil {
			t.Fatal(err)
		}
	}
	if fake.milestoneCalls != 1 {
		t.Errorf("milestone list fetched %d times, want 1", fake.milestoneCalls)
	}
	if fake.createdBody["milestone"] != float64(3) {
		t.Errorf("milestone = %v, want 3", fake.createdBody["milestone"])
	}
}

func TestQuotaWatcherWarnsOncePerWindow(t *testing.T) {
	q := &quotaWatcher{installation: 42}
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", "400")
	h.Set("X-RateLimit-Reset", "1700000000")

	q.observe(h)
	if q.warnedReset != "1700000000" {
		t.Fatalf("no warning recorded below 10%%")
	}
	q.warnedReset = "sentinel"
	h.Set("X-RateLimit-Remaining", "4000")
	q.observe(h)
	if q.warnedReset != "sentinel" {
		t.Error("warned with plenty of quota left")
	}
}
