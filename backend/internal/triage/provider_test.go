package triage

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

type mockFileFetcher struct {
	mu    sync.Mutex
	calls map[string]int
	files map[string]map[string][]byte
	errs  map[string]map[string]error
	delay time.Duration
}

func newMockFileFetcher() *mockFileFetcher {
	return &mockFileFetcher{
		calls: make(map[string]int),
		files: make(map[string]map[string][]byte),
		errs:  make(map[string]map[string]error),
	}
}

func (m *mockFileFetcher) setFile(repo, path string, content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.files[repo] == nil {
		m.files[repo] = make(map[string][]byte)
	}
	m.files[repo][path] = content
}

func (m *mockFileFetcher) setError(repo, path string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.errs[repo] == nil {
		m.errs[repo] = make(map[string]error)
	}
	m.errs[repo][path] = err
}

func (m *mockFileFetcher) callCount(repo string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[repo]
}

func (m *mockFileFetcher) totalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, c := range m.calls {
		total += c
	}
	return total
}

func (m *mockFileFetcher) FetchFile(ctx context.Context, repo, path string) ([]byte, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	m.mu.Lock()
	m.calls[repo]++
	if errMap, ok := m.errs[repo]; ok {
		if err, hasErr := errMap[path]; hasErr {
			m.mu.Unlock()
			return nil, err
		}
	}
	if fileMap, ok := m.files[repo]; ok {
		if data, hasData := fileMap[path]; hasData {
			m.mu.Unlock()
			return data, nil
		}
	}
	m.mu.Unlock()

	return nil, os.ErrNotExist
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

func TestCodeownersProvider_SingleflightDeduplication(t *testing.T) {
	mock := newMockFileFetcher()
	mock.setFile("org/app", ".github/CODEOWNERS", []byte("* @core-team\n*.swift @ios-team\n"))
	// Artificially delay fetch so all 50 concurrent requests pile up in singleflight
	mock.delay = 30 * time.Millisecond

	provider := NewCodeownersProvider(mock)

	const concurrency = 50
	var startGate sync.WaitGroup
	startGate.Add(1)

	var wg sync.WaitGroup
	results := make([]*Codeowners, concurrency)
	errs := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait() // Synchronize release
			results[idx], errs[idx] = provider.GetCodeowners(context.Background(), "org/app")
		}(i)
	}

	startGate.Done() // Unleash all 50 goroutines simultaneously
	wg.Wait()

	for i := 0; i < concurrency; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d got error: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Fatalf("goroutine %d got nil result", i)
		}
		owners, pattern := results[i].Match("CartView.swift")
		if pattern != "*.swift" || len(owners) == 0 || owners[0] != "@ios-team" {
			t.Errorf("goroutine %d got unexpected match: %v, %s", i, owners, pattern)
		}
	}

	// Verify that mock was invoked exactly once for the repository
	totalCalls := mock.callCount("org/app")
	if totalCalls != 1 {
		t.Errorf("expected exactly 1 fetch call across 50 concurrent callers, got %d", totalCalls)
	}
}

func TestCodeownersProvider_CacheExpiration(t *testing.T) {
	mock := newMockFileFetcher()
	mock.setFile("org/cache-app", ".github/CODEOWNERS", []byte("* @core-team\n"))

	clk := &threadSafeClock{now: time.Now()}
	provider := NewCodeownersProvider(mock, WithTTL(5*time.Minute), WithNow(clk.Now))

	ctx := context.Background()

	// Initial fetch
	co1, err := provider.GetCodeowners(ctx, "org/cache-app")
	if err != nil || co1 == nil {
		t.Fatalf("initial fetch failed: %v", err)
	}
	if calls := mock.callCount("org/cache-app"); calls != 1 {
		t.Fatalf("expected 1 call after initial fetch, got %d", calls)
	}

	// Fetch within TTL (advance clock by 2 minutes)
	clk.Advance(2 * time.Minute)
	co2, err := provider.GetCodeowners(ctx, "org/cache-app")
	if err != nil || co2 != co1 {
		t.Fatalf("expected cached *Codeowners, got %v (err: %v)", co2, err)
	}
	if calls := mock.callCount("org/cache-app"); calls != 1 {
		t.Fatalf("expected still 1 call (cache hit), got %d", calls)
	}

	// Fetch after TTL expiration (advance clock by another 4 minutes, total 6 > 5)
	clk.Advance(4 * time.Minute)
	co3, err := provider.GetCodeowners(ctx, "org/cache-app")
	if err != nil || co3 == nil {
		t.Fatalf("fetch after expiration failed: %v", err)
	}
	if calls := mock.callCount("org/cache-app"); calls != 2 {
		t.Fatalf("expected 2 calls after expiration, got %d", calls)
	}
}

func TestCodeownersProvider_GracefulFallback(t *testing.T) {
	t.Run("returns nil without error when file not found", func(t *testing.T) {
		mock := newMockFileFetcher() // empty, returns os.ErrNotExist for all paths
		provider := NewCodeownersProvider(mock)

		co, err := provider.GetCodeowners(context.Background(), "org/empty-repo")
		if err != nil {
			t.Errorf("expected nil error on missing file, got %v", err)
		}
		if co != nil {
			t.Errorf("expected nil Codeowners on missing file, got %+v", co)
		}
	})

	t.Run("survives unexpected fetcher errors gracefully", func(t *testing.T) {
		mock := newMockFileFetcher()
		fetchErr := errors.New("upstream rate limit exceeded")
		mock.setError("org/err-repo", ".github/CODEOWNERS", fetchErr)
		mock.setError("org/err-repo", "CODEOWNERS", fetchErr)
		mock.setError("org/err-repo", "docs/CODEOWNERS", fetchErr)

		provider := NewCodeownersProvider(mock)

		co, err := provider.GetCodeowners(context.Background(), "org/err-repo")
		if err != nil {
			t.Errorf("expected nil error (graceful fallback), got %v", err)
		}
		if co != nil {
			t.Errorf("expected nil Codeowners, got %+v", co)
		}
	})

	t.Run("handles nil receiver or empty repository safely", func(t *testing.T) {
		var nilProvider *CodeownersProvider
		co, err := nilProvider.GetCodeowners(context.Background(), "org/app")
		if err != nil || co != nil {
			t.Errorf("expected nil, nil on nil provider, got %v, %v", co, err)
		}

		mock := newMockFileFetcher()
		provider := NewCodeownersProvider(mock)
		co, err = provider.GetCodeowners(context.Background(), "")
		if err != nil || co != nil {
			t.Errorf("expected nil, nil on empty repo, got %v, %v", co, err)
		}
	})
}

func TestCodeownersProvider_CandidateLocationsOrder(t *testing.T) {
	t.Run("stops at .github/CODEOWNERS when present", func(t *testing.T) {
		mock := newMockFileFetcher()
		mock.setFile("org/multi", ".github/CODEOWNERS", []byte("* @github-owners\n"))
		mock.setFile("org/multi", "CODEOWNERS", []byte("* @root-owners\n"))

		provider := NewCodeownersProvider(mock)
		co, err := provider.GetCodeowners(context.Background(), "org/multi")
		if err != nil || co == nil {
			t.Fatalf("expected valid codeowners, got err: %v", err)
		}
		owners, _ := co.Match("any.go")
		if len(owners) == 0 || owners[0] != "@github-owners" {
			t.Errorf("expected .github/CODEOWNERS to take precedence, got %v", owners)
		}
		if calls := mock.callCount("org/multi"); calls != 1 {
			t.Errorf("expected exactly 1 call (stopped at first found), got %d", calls)
		}
	})

	t.Run("falls back to root CODEOWNERS when .github is missing", func(t *testing.T) {
		mock := newMockFileFetcher()
		mock.setFile("org/root-only", "CODEOWNERS", []byte("* @root-owners\n"))

		provider := NewCodeownersProvider(mock)
		co, err := provider.GetCodeowners(context.Background(), "org/root-only")
		if err != nil || co == nil {
			t.Fatalf("expected valid codeowners, got err: %v", err)
		}
		owners, _ := co.Match("any.go")
		if len(owners) == 0 || owners[0] != "@root-owners" {
			t.Errorf("expected root CODEOWNERS, got %v", owners)
		}
		if calls := mock.callCount("org/root-only"); calls != 2 {
			t.Errorf("expected 2 calls (.github tried, then root), got %d", calls)
		}
	})

	t.Run("falls back to docs/CODEOWNERS when .github and root are missing", func(t *testing.T) {
		mock := newMockFileFetcher()
		mock.setFile("org/docs-only", "docs/CODEOWNERS", []byte("* @docs-owners\n"))

		provider := NewCodeownersProvider(mock)
		co, err := provider.GetCodeowners(context.Background(), "org/docs-only")
		if err != nil || co == nil {
			t.Fatalf("expected valid codeowners, got err: %v", err)
		}
		owners, _ := co.Match("any.go")
		if len(owners) == 0 || owners[0] != "@docs-owners" {
			t.Errorf("expected docs CODEOWNERS, got %v", owners)
		}
		if calls := mock.callCount("org/docs-only"); calls != 3 {
			t.Errorf("expected 3 calls, got %d", calls)
		}
	})
}
