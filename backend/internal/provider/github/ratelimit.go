package github

// GitHub rate limits, and how this package respects them.
//
// Primary limit: each App installation has an hourly REST quota (5,000
// requests, more for large organisations). Exceeding it returns 403/429 with
// X-RateLimit-Remaining: 0 and X-RateLimit-Reset. go-github turns that into
// *github.RateLimitError and, until the reset, refuses further calls on that
// client without contacting GitHub.
//
// Secondary limits: GitHub also limits bursts and concurrency, and is
// strictest about "content creation" such as opening issues and comments
// (about 80 per minute and 500 per hour). It asks integrations to make write
// requests one at a time with at least one second between them. Exceeding a
// secondary limit returns 403/429 that go-github turns into
// *github.AbuseRateLimitError, usually with a Retry-After.
//
// What this package does:
//
//   - pacer: per installation, write requests (POST/PATCH/PUT/DELETE) are
//     serialised and spaced at least MinWriteInterval apart.
//   - call: when GitHub reports a rate limit, the request was not processed,
//     so it is retried after the advertised wait, if that wait fits within
//     MaxRateLimitWait and the context deadline, up to maxAttempts times.
//     Otherwise call returns *issue.RateLimitError carrying the wait, which
//     upper layers surface (HTTP 429 + Retry-After) or reschedule. Other
//     errors are never retried here: retrying a failed POST could open a
//     duplicate issue.
//   - quotaWatcher: logs a warning once per reset window when an
//     installation's remaining primary quota drops below 10%.
//   - milestone titles are cached per repository (see opener.go) so opening
//     an issue with a milestone usually costs one request, not two.

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	gh "github.com/google/go-github/v93/github"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

const (
	// DefaultMinWriteInterval is GitHub's recommended spacing between write
	// requests.
	DefaultMinWriteInterval = time.Second
	// DefaultMaxRateLimitWait bounds how long a single call may wait out a
	// rate limit before giving up with *issue.RateLimitError.
	DefaultMaxRateLimitWait = 30 * time.Second

	maxAttempts = 3
	// GitHub asks to wait at least a minute after a secondary limit that
	// came without Retry-After.
	defaultSecondaryWait = time.Minute
	// quotaWarnDivisor: warn when remaining < limit / quotaWarnDivisor.
	quotaWarnDivisor = 10
)

// pacer spaces calls at least interval apart. Callers reserve consecutive
// slots, so concurrent writers are serialised rather than bunched.
type pacer struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
}

// wait blocks until the caller's slot. A nil pacer or zero interval never
// waits.
func (p *pacer) wait(ctx context.Context) error {
	if p == nil || p.interval <= 0 {
		return nil
	}
	p.mu.Lock()
	now := time.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.interval)
	p.mu.Unlock()
	return sleepCtx(ctx, time.Until(at))
}

// call runs op and handles GitHub rate limits as described above. writes
// is the pacer to wait on before each attempt (nil for reads); id names
// the installation in logs.
func (p *Provider) call(ctx context.Context, id int64, writes *pacer, op func() error) error {
	for attempt := 1; ; attempt++ {
		if err := writes.wait(ctx); err != nil {
			return err
		}
		err := op()
		wait, secondary, limited := rateLimitWait(err)
		if !limited {
			return err
		}

		rl := &issue.RateLimitError{Tracker: "github", Wait: wait, Secondary: secondary, Err: err}
		if attempt >= maxAttempts || wait > p.opts.MaxRateLimitWait || !fitsDeadline(ctx, wait) {
			slog.WarnContext(ctx, "github rate limit: giving up",
				"installation", id, "secondary", secondary, "retry_after", wait, "attempt", attempt)
			return rl
		}
		slog.WarnContext(ctx, "github rate limit: waiting before retry",
			"installation", id, "secondary", secondary, "wait", wait, "attempt", attempt)
		if err := sleepCtx(ctx, wait); err != nil {
			return rl
		}
	}
}

// rateLimitWait reports whether err is a GitHub rate limit and how long to
// wait before retrying.
func rateLimitWait(err error) (wait time.Duration, secondary, limited bool) {
	var abuse *gh.AbuseRateLimitError
	if errors.As(err, &abuse) {
		if abuse.RetryAfter != nil && *abuse.RetryAfter > 0 {
			return *abuse.RetryAfter, true, true
		}
		return defaultSecondaryWait, true, true
	}
	var primary *gh.RateLimitError
	if errors.As(err, &primary) {
		wait := time.Until(primary.Rate.Reset.Time) + time.Second
		if wait < time.Second {
			wait = time.Second
		}
		return wait, false, true
	}
	return 0, false, false
}

func fitsDeadline(ctx context.Context, wait time.Duration) bool {
	deadline, ok := ctx.Deadline()
	return !ok || time.Until(deadline) > wait
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// quotaWatcher is an http.RoundTripper that reads GitHub's rate-limit
// headers and warns once per reset window when an installation is running
// low on its primary quota.
type quotaWatcher struct {
	base         http.RoundTripper
	installation int64

	mu          sync.Mutex
	warnedReset string
}

func (q *quotaWatcher) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := q.base.RoundTrip(req)
	if err == nil {
		q.observe(resp.Header)
	}
	return resp, err
}

func (q *quotaWatcher) observe(h http.Header) {
	limit, err1 := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, err2 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err1 != nil || err2 != nil || limit <= 0 {
		return
	}
	if remaining*quotaWarnDivisor >= limit {
		return
	}
	reset := h.Get("X-RateLimit-Reset")
	q.mu.Lock()
	already := q.warnedReset == reset
	q.warnedReset = reset
	q.mu.Unlock()
	if already {
		return
	}
	resetAt := ""
	if secs, err := strconv.ParseInt(reset, 10, 64); err == nil {
		resetAt = time.Unix(secs, 0).UTC().Format(time.RFC3339)
	}
	slog.Warn("github installation is low on API quota",
		"installation", q.installation,
		"resource", h.Get("X-RateLimit-Resource"),
		"remaining", remaining, "limit", limit, "reset", resetAt)
}
