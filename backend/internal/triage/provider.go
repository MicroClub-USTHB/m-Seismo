package triage

import (
	"context"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultCodeownersTTL is the default cache retention period for CODEOWNERS files.
const DefaultCodeownersTTL = 5 * time.Minute

// candidateCodeownersPaths defines the standard locations tried in order.
var candidateCodeownersPaths = []string{
	".github/CODEOWNERS",
	"CODEOWNERS",
	"docs/CODEOWNERS",
}

// FileFetcher fetches raw file contents from a repository at a given ref/path.
type FileFetcher interface {
	FetchFile(ctx context.Context, repository string, path string) ([]byte, error)
}

type codeownersCacheEntry struct {
	co        *Codeowners
	expiresAt time.Time
}

// CodeownersProvider provides cached and deduplicated CODEOWNERS files for repositories.
type CodeownersProvider struct {
	fetcher FileFetcher
	ttl     time.Duration
	group   singleflight.Group
	mu      sync.RWMutex
	cache   map[string]codeownersCacheEntry
	now     func() time.Time
}

// ProviderOption configures a CodeownersProvider.
type ProviderOption func(*CodeownersProvider)

// WithTTL sets a custom cache expiration duration.
func WithTTL(d time.Duration) ProviderOption {
	return func(p *CodeownersProvider) {
		if d > 0 {
			p.ttl = d
		}
	}
}

// WithNow sets a custom clock function for deterministic tests.
func WithNow(fn func() time.Time) ProviderOption {
	return func(p *CodeownersProvider) {
		if fn != nil {
			p.now = fn
		}
	}
}

// NewCodeownersProvider creates a new provider with in-memory caching and singleflight deduplication.
func NewCodeownersProvider(fetcher FileFetcher, opts ...ProviderOption) *CodeownersProvider {
	p := &CodeownersProvider{
		fetcher: fetcher,
		ttl:     DefaultCodeownersTTL,
		cache:   make(map[string]codeownersCacheEntry),
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// GetCodeowners retrieves the CODEOWNERS for a repository, using singleflight deduplication
// to prevent thundering herd spikes and an in-memory TTL cache to minimize network calls.
func (p *CodeownersProvider) GetCodeowners(ctx context.Context, repository string) (*Codeowners, error) {
	if p == nil || p.fetcher == nil || repository == "" {
		return nil, nil
	}

	now := p.now()
	p.mu.RLock()
	entry, found := p.cache[repository]
	p.mu.RUnlock()

	if found && now.Before(entry.expiresAt) {
		return entry.co, nil
	}

	val, err, _ := p.group.Do(repository, func() (any, error) {
		// Double-check cache in case a concurrent call resolved it while waiting
		now := p.now()
		p.mu.RLock()
		entry, found := p.cache[repository]
		p.mu.RUnlock()
		if found && now.Before(entry.expiresAt) {
			return entry.co, nil
		}

		var foundBytes []byte
		for _, loc := range candidateCodeownersPaths {
			data, fetchErr := p.fetcher.FetchFile(ctx, repository, loc)
			if fetchErr == nil && len(data) > 0 {
				foundBytes = data
				break
			}
		}

		var co *Codeowners
		if len(foundBytes) > 0 {
			co = ParseCodeowners(foundBytes)
		}

		p.mu.Lock()
		p.cache[repository] = codeownersCacheEntry{
			co:        co,
			expiresAt: p.now().Add(p.ttl),
		}
		p.mu.Unlock()

		return co, nil
	})

	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil
	}

	co, ok := val.(*Codeowners)
	if !ok {
		return nil, os.ErrInvalid
	}
	return co, nil
}

// Invalidate removes a cached repository entry.
func (p *CodeownersProvider) Invalidate(repository string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	delete(p.cache, repository)
	p.mu.Unlock()
}
