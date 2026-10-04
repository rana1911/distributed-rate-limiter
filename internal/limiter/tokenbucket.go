// Package limiter implements the two rate-limiting algorithms, each backed
// by an atomic Redis Lua script -- direct Go equivalent of
// src/limiters/tokenBucket.js and src/limiters/slidingWindow.js in the
// Node version. The Lua script text and the atomicity reasoning are
// unchanged from the Node version: the whole point is that "read current
// state, compute new state, write it back" happens as one uninterruptible
// operation inside Redis, so concurrent requests -- even from separate
// goroutines on separate app instances -- can't race each other into
// over-allowing a client past their limit.
package limiter

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketScript is functionally identical to the Lua script in the
// Node version's tokenBucket.js -- same fields, same refill math, same
// EXPIRE-based cleanup for idle clients.
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refillRate = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local bucket = redis.call('HMGET', key, 'tokens', 'timestamp')
local tokens = tonumber(bucket[1])
local timestamp = tonumber(bucket[2])

if tokens == nil then
  tokens = capacity
  timestamp = now
end

local elapsedSec = math.max(0, (now - timestamp) / 1000)
tokens = math.min(capacity, tokens + elapsedSec * refillRate)

local allowed = 0
if tokens >= requested then
  tokens = tokens - requested
  allowed = 1
end

redis.call('HMSET', key, 'tokens', tokens, 'timestamp', now)
redis.call('EXPIRE', key, math.ceil(capacity / refillRate) * 2)

return { allowed, tostring(tokens) }
`)

// TokenBucketOptions mirrors the { capacity, refillRatePerSec, cost }
// options object passed to checkTokenBucket in the Node version.
type TokenBucketOptions struct {
	Capacity         float64
	RefillRatePerSec float64
	Cost             float64 // defaults to 1 if zero, see CheckTokenBucket
}

// Result mirrors the { allowed, remaining } object returned by the Node
// version's checkTokenBucket.
type Result struct {
	Allowed   bool
	Remaining float64
}

// CheckTokenBucket runs the token bucket check atomically against Redis.
//
// Note: unlike the Node version, we don't manually manage EVALSHA/SCRIPT
// LOAD or handle NOSCRIPT errors ourselves -- go-redis's *redis.Script
// type (created above with redis.NewScript) does this automatically:
// Script.Run tries EVALSHA first and transparently falls back to EVAL
// (which also loads the script) if Redis doesn't recognize the hash yet.
// Same end behavior as the Node version's manual retry logic, less code.
func CheckTokenBucket(ctx context.Context, rdb *redis.Client, key string, opts TokenBucketOptions) (Result, error) {
	cost := opts.Cost
	if cost == 0 {
		cost = 1
	}
	now := float64(time.Now().UnixMilli())

	res, err := tokenBucketScript.Run(ctx, rdb, []string{key},
		opts.Capacity, opts.RefillRatePerSec, now, cost,
	).Result()
	if err != nil {
		return Result{}, err
	}

	// The script returns a two-element array: [allowed (0/1), remaining tokens as string].
	arr := res.([]interface{})
	allowed := arr[0].(int64) == 1
	remaining, err := parseFloat(arr[1].(string))
	if err != nil {
		return Result{}, err
	}

	return Result{Allowed: allowed, Remaining: remaining}, nil
}
