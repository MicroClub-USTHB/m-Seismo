package release

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockReleaseClient struct {
	mu    sync.Mutex
	calls map[string]int
	tags  map[string][]string
	errs  map[string]error
	delay time.Duration
}

func newMockReleaseClient() *mockReleaseClient {
	return &mockReleaseClient{
		calls: make(map[string]int),
		tags:  make(map[string][]string),
		errs:  make(map[string]error),
	}
}

func (m *mockReleaseClient) setTags(repo string, tags []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tags[repo] = tags
}

func (m *mockReleaseClient) setError(repo string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errs[repo] = err
}

func (m *mockReleaseClient) callCount(repo string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[repo]
}

func (m *mockReleaseClient) ListTags(ctx context.Context, tracker, repository string) ([]string, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	m.mu.Lock()
	m.calls[repository]++
	if err, ok := m.errs[repository]; ok {
		m.mu.Unlock()
		return nil, err
	}
	tags := m.tags[repository]
	m.mu.Unlock()

	return tags, nil
}

type threadSafeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *threadSafeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *threadSafeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestCorrelator_GitHub(t *testing.T) {
	client := newMockReleaseClient()
	client.setTags("owner/repo", []string{"v1.4.0", "v1.3.0", "v1.2.0"})

	correlator := NewCorrelator(WithReleaseClient(client))

	req := CorrelateRequest{
		Tracker:    "github",
		Repository: "owner/repo",
		Version:    "1.4.0",
		Build:      "8a9b0c1d2e",
	}

	rel, err := correlator.Correlate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rel == nil {
		t.Fatal("expected non-nil CorrelatedRelease")
	}

	if rel.TagName != "v1.4.0" {
		t.Errorf("expected tag v1.4.0, got %s", rel.TagName)
	}
	if rel.PreviousTag != "v1.3.0" {
		t.Errorf("expected previous tag v1.3.0, got %s", rel.PreviousTag)
	}
	if rel.ReleaseURL != "https://github.com/owner/repo/releases/tag/v1.4.0" {
		t.Errorf("unexpected ReleaseURL: %s", rel.ReleaseURL)
	}
	if rel.CompareURL != "https://github.com/owner/repo/compare/v1.3.0...v1.4.0" {
		t.Errorf("unexpected CompareURL: %s", rel.CompareURL)
	}
	if rel.CommitURL != "https://github.com/owner/repo/commit/8a9b0c1d2e" {
		t.Errorf("unexpected CommitURL: %s", rel.CommitURL)
	}
}

func TestCorrelator_GitLab(t *testing.T) {
	client := newMockReleaseClient()
	client.setTags("group/subgroup/project", []string{"v2.0.0", "v1.9.0"})

	correlator := NewCorrelator(WithReleaseClient(client))

	req := CorrelateRequest{
		Tracker:    "gitlab",
		Repository: "https://gitlab.com/group/subgroup/project.git",
		Version:    "v2.0.0",
		Build:      "f00baa1",
	}

	rel, err := correlator.Correlate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rel.ReleaseURL != "https://gitlab.com/group/subgroup/project/-/releases/v2.0.0" {
		t.Errorf("unexpected GitLab ReleaseURL: %s", rel.ReleaseURL)
	}
	if rel.CompareURL != "https://gitlab.com/group/subgroup/project/-/compare/v1.9.0...v2.0.0" {
		t.Errorf("unexpected GitLab CompareURL: %s", rel.CompareURL)
	}
	if rel.CommitURL != "https://gitlab.com/group/subgroup/project/-/commit/f00baa1" {
		t.Errorf("unexpected GitLab CommitURL: %s", rel.CommitURL)
	}
}

func TestCorrelator_Bitbucket(t *testing.T) {
	client := newMockReleaseClient()
	client.setTags("workspace/repo", []string{"v1.0.1", "v1.0.0"})

	correlator := NewCorrelator(WithReleaseClient(client))

	req := CorrelateRequest{
		Tracker:    "bitbucket",
		Repository: "workspace/repo",
		Version:    "1.0.1",
		Build:      "82", // numeric build, not a git commit hash
	}

	rel, err := correlator.Correlate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rel.ReleaseURL != "https://bitbucket.org/workspace/repo/src/v1.0.1" {
		t.Errorf("unexpected Bitbucket ReleaseURL: %s", rel.ReleaseURL)
	}
	if rel.CompareURL != "https://bitbucket.org/workspace/repo/branches/compare/v1.0.1%0Dv1.0.0" {
		t.Errorf("unexpected Bitbucket CompareURL: %s", rel.CompareURL)
	}
	if rel.CommitURL != "" {
		t.Errorf("expected empty CommitURL for non-commit build, got: %s", rel.CommitURL)
	}
}

func TestCorrelator_SingleflightDeduplication(t *testing.T) {
	client := newMockReleaseClient()
	client.setTags("org/high-volume", []string{"v3.1.0", "v3.0.0"})
	client.delay = 30 * time.Millisecond // simulate network latency to pile up concurrent calls

	correlator := NewCorrelator(WithReleaseClient(client))

	const concurrency = 50
	var startGate sync.WaitGroup
	startGate.Add(1)

	var wg sync.WaitGroup
	results := make([]*CorrelatedRelease, concurrency)
	errs := make([]error, concurrency)

	req := CorrelateRequest{
		Tracker:    "github",
		Repository: "org/high-volume",
		Version:    "3.1.0",
		Build:      "102",
	}

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()
			results[idx], errs[idx] = correlator.Correlate(context.Background(), req)
		}(i)
	}

	startGate.Done() // release all 50 simultaneously
	wg.Wait()

	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d got error: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d got nil result", i)
		}
		if results[i].CompareURL != "https://github.com/org/high-volume/compare/v3.0.0...v3.1.0" {
			t.Errorf("goroutine %d unexpected CompareURL: %s", i, results[i].CompareURL)
		}
	}

	// Verify that client was called exactly once despite 50 concurrent requests
	calls := client.callCount("org/high-volume")
	if calls != 1 {
		t.Errorf("expected exactly 1 client call across 50 concurrent requests, got %d", calls)
	}
}

func TestCorrelator_CacheExpiration(t *testing.T) {
	client := newMockReleaseClient()
	client.setTags("org/app", []string{"v1.0.0"})

	clk := &threadSafeClock{now: time.Now()}
	cache := NewCache(WithCacheTTL(15*time.Minute), WithCacheNow(clk.Now))
	correlator := NewCorrelator(WithReleaseClient(client), WithReleaseCache(cache))

	req := CorrelateRequest{
		Tracker:    "github",
		Repository: "org/app",
		Version:    "1.0.0",
	}

	ctx := context.Background()

	// Initial fetch
	rel1, err := correlator.Correlate(ctx, req)
	if err != nil || rel1 == nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if calls := client.callCount("org/app"); calls != 1 {
		t.Fatalf("expected 1 call after initial fetch, got %d", calls)
	}

	// Fetch within TTL (advance clock by 5m < 15m TTL)
	clk.Advance(5 * time.Minute)
	rel2, err := correlator.Correlate(ctx, req)
	if err != nil || rel2 != rel1 {
		t.Fatalf("expected cached release, got %v (err: %v)", rel2, err)
	}
	if calls := client.callCount("org/app"); calls != 1 {
		t.Fatalf("expected still 1 call (cache hit), got %d", calls)
	}

	// Fetch after TTL expiration (advance clock by another 12m, total 17m > 15m)
	clk.Advance(12 * time.Minute)
	rel3, err := correlator.Correlate(ctx, req)
	if err != nil || rel3 == nil {
		t.Fatalf("fetch after expiration failed: %v", err)
	}
	if calls := client.callCount("org/app"); calls != 2 {
		t.Fatalf("expected 2 calls after expiration, got %d", calls)
	}
}

func TestCorrelator_GracefulFallbackAndEdgeCases(t *testing.T) {
	t.Run("nil client falls back to deterministic URLs", func(t *testing.T) {
		correlator := NewCorrelator() // no client
		req := CorrelateRequest{
			Tracker:    "github",
			Repository: "my-org/my-repo",
			Version:    "1.2.3",
		}
		rel, err := correlator.Correlate(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rel.ReleaseURL != "https://github.com/my-org/my-repo/releases/tag/v1.2.3" {
			t.Errorf("unexpected ReleaseURL: %s", rel.ReleaseURL)
		}
		if rel.CompareURL != "" {
			t.Errorf("expected empty CompareURL when previous tag unknown, got %s", rel.CompareURL)
		}
	})

	t.Run("client error falls back gracefully without failing", func(t *testing.T) {
		client := newMockReleaseClient()
		client.setError("broken/repo", errors.New("github rate limit 403"))
		correlator := NewCorrelator(WithReleaseClient(client))

		req := CorrelateRequest{
			Tracker:    "github",
			Repository: "broken/repo",
			Version:    "v5.0.0",
		}
		rel, err := correlator.Correlate(context.Background(), req)
		if err != nil {
			t.Fatalf("expected graceful fallback, got error: %v", err)
		}
		if rel.ReleaseURL != "https://github.com/broken/repo/releases/tag/v5.0.0" {
			t.Errorf("unexpected ReleaseURL: %s", rel.ReleaseURL)
		}
	})

	t.Run("empty version or repository returns nil", func(t *testing.T) {
		correlator := NewCorrelator()
		rel, err := correlator.Correlate(context.Background(), CorrelateRequest{Version: "", Repository: "org/app"})
		if err != nil || rel != nil {
			t.Errorf("expected nil for empty version, got %v, %v", rel, err)
		}

		rel, err = correlator.Correlate(context.Background(), CorrelateRequest{Version: "1.0", Repository: ""})
		if err != nil || rel != nil {
			t.Errorf("expected nil for empty repository, got %v, %v", rel, err)
		}
	})
}
