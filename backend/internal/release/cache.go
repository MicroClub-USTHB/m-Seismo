package release

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultTTL is the default cache expiration duration for release lookups.
const DefaultTTL = 20 * time.Minute

type cacheEntry struct {
	release   *CorrelatedRelease
	expiresAt time.Time
}

// Cache provides a thread-safe in-memory cache with singleflight deduplication.
type Cache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	group singleflight.Group
	items map[string]cacheEntry
	now   func() time.Time
}

// CacheOption configures a Cache.
type CacheOption func(*Cache)

// WithCacheTTL sets a custom cache expiration duration.
func WithCacheTTL(d time.Duration) CacheOption {
	return func(c *Cache) {
		if d > 0 {
			c.ttl = d
		}
	}
}

// WithCacheNow sets a custom clock function for deterministic tests.
func WithCacheNow(fn func() time.Time) CacheOption {
	return func(c *Cache) {
		if fn != nil {
			c.now = fn
		}
	}
}

// NewCache creates a new Cache with the given options.
func NewCache(opts ...CacheOption) *Cache {
	c := &Cache{
		ttl:   DefaultTTL,
		items: make(map[string]cacheEntry),
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetOrCompute retrieves a cached CorrelatedRelease or computes it using singleflight deduplication.
func (c *Cache) GetOrCompute(key string, compute func() (*CorrelatedRelease, error)) (*CorrelatedRelease, error) {
	if c == nil {
		return compute()
	}

	now := c.now()
	c.mu.RLock()
	entry, found := c.items[key]
	c.mu.RUnlock()

	if found && now.Before(entry.expiresAt) {
		return entry.release, nil
	}

	val, err, _ := c.group.Do(key, func() (any, error) {
		// Double check cache in case another concurrent goroutine populated it
		now := c.now()
		c.mu.RLock()
		entry, found := c.items[key]
		c.mu.RUnlock()
		if found && now.Before(entry.expiresAt) {
			return entry.release, nil
		}

		res, computeErr := compute()
		if computeErr != nil {
			return nil, computeErr
		}

		c.mu.Lock()
		c.items[key] = cacheEntry{
			release:   res,
			expiresAt: c.now().Add(c.ttl),
		}
		c.mu.Unlock()

		return res, nil
	})

	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, nil
	}

	return val.(*CorrelatedRelease), nil
}

// Invalidate removes a specific key from the cache.
func (c *Cache) Invalidate(key string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.items, key)
	c.mu.Unlock()
}

// Clear empties all entries from the cache.
func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	clear(c.items)
	c.mu.Unlock()
}
