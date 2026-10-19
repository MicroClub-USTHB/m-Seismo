// Package ratelimit limits how often m-Seismo's own actions may run, using
// fixed windows counted in Redis so the limit holds across server
// instances.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

// Module provides a Redis-backed Limiter.
var Module = fx.Module("ratelimit",
	fx.Provide(NewRedis),
)

// Limiter decides whether one more event is allowed for key.
type Limiter interface {
	// Allow counts one event for key in the current window. It reports
	// whether the event is within limit and, when it is not, how long until
	// the window resets. A limit <= 0 always allows.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ok bool, retryAfter time.Duration, err error)
}

const keyPrefix = "ratelimit:"

// fixedWindow increments the counter and starts its expiry on the first
// event of a window, atomically. Returns {count, ttl_ms}.
var fixedWindow = redis.NewScript(`
local n = redis.call('INCR', KEYS[1])
if n == 1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return {n, redis.call('PTTL', KEYS[1])}
`)

type redisLimiter struct{ rdb *redis.Client }

func NewRedis(rdb *redis.Client) Limiter { return redisLimiter{rdb: rdb} }

func (l redisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, time.Duration, error) {
	if limit <= 0 {
		return true, 0, nil
	}
	res, err := fixedWindow.Run(ctx, l.rdb, []string{keyPrefix + key}, window.Milliseconds()).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("ratelimit: %w", err)
	}
	count, ttl := res[0], time.Duration(res[1])*time.Millisecond
	if count <= int64(limit) {
		return true, 0, nil
	}
	if ttl <= 0 {
		ttl = window
	}
	return false, ttl, nil
}
