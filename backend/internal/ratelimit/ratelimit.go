// Package ratelimit limits how often something may happen, shared by every
// backend instance through Redis.
//
// Each key has a token bucket: it holds up to Burst tokens and refills at
// PerMinute tokens per minute. Every allowed call takes one token. The
// bucket is updated by a Lua script, so concurrent calls from several
// instances never over-allow.
package ratelimit

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"
)

// Module provides the Redis-backed *Limiter shared by the API.
var Module = fx.Module("ratelimit",
	fx.Provide(func(rdb *redis.Client) *Limiter { return New(rdb, "seismo:ratelimit:") }),
)

// Limit is how often a key may be used.
type Limit struct {
	// PerMinute is the steady rate.
	PerMinute float64
	// Burst is how many calls may happen at once after a quiet period.
	Burst int
}

// Result is the outcome of one call to Allow.
type Result struct {
	Allowed bool
	// RetryAfter is how long to wait before the next call can succeed. Zero
	// when Allowed.
	RetryAfter time.Duration
}

// script refills the bucket for the time elapsed since its last use, then
// takes a token if one is available. It uses the Redis clock so every
// instance sees the same time.
var script = redis.NewScript(`
local rate = tonumber(ARGV[1])
local burst = tonumber(ARGV[2])
local t = redis.call('TIME')
local now = tonumber(t[1]) + tonumber(t[2]) / 1000000

local state = redis.call('HMGET', KEYS[1], 'tokens', 'ts')
local tokens = tonumber(state[1])
local ts = tonumber(state[2])
if tokens == nil then
  tokens = burst
  ts = now
end
tokens = math.min(burst, tokens + (now - ts) * rate)

local allowed = 0
local wait = 0
if tokens >= 1 then
  tokens = tokens - 1
  allowed = 1
else
  wait = (1 - tokens) / rate
end

redis.call('HSET', KEYS[1], 'tokens', tostring(tokens), 'ts', tostring(now))
redis.call('EXPIRE', KEYS[1], math.ceil(burst / rate) + 1)
return {allowed, tostring(wait)}
`)

// Limiter checks limits stored in Redis.
type Limiter struct {
	rdb    *redis.Client
	prefix string
}

// New returns a Limiter whose Redis keys start with prefix.
func New(rdb *redis.Client, prefix string) *Limiter {
	return &Limiter{rdb: rdb, prefix: prefix}
}

// Allow takes one token from key's bucket.
func (l *Limiter) Allow(ctx context.Context, key string, lim Limit) (Result, error) {
	if lim.PerMinute <= 0 || lim.Burst < 1 {
		return Result{}, fmt.Errorf("ratelimit: invalid limit %+v", lim)
	}
	perSecond := lim.PerMinute / 60

	out, err := script.Run(ctx, l.rdb, []string{l.prefix + key}, perSecond, lim.Burst).Slice()
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: %w", err)
	}
	allowed, _ := out[0].(int64)
	waitText, _ := out[1].(string)
	wait, err := strconv.ParseFloat(waitText, 64)
	if err != nil {
		return Result{}, fmt.Errorf("ratelimit: bad wait %q: %w", waitText, err)
	}

	if allowed == 1 {
		return Result{Allowed: true}, nil
	}
	return Result{RetryAfter: time.Duration(math.Ceil(wait*1000)) * time.Millisecond}, nil
}
