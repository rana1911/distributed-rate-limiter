package limiter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// slidingWindowScript is the direct Go equivalent of the Lua script in
// the Node version's slidingWindow.js -- same weighted current+previous
// window approximation, same tradeoff (O(1) memory per client instead of
// storing every request timestamp).
var slidingWindowScript = redis.NewScript(`
local currKey = KEYS[1]
local prevKey = KEYS[2]
local windowMs = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

local currCount = tonumber(redis.call('GET', currKey) or '0')
local prevCount = tonumber(redis.call('GET', prevKey) or '0')

local elapsedInWindow = now % windowMs
local weight = (windowMs - elapsedInWindow) / windowMs

local estimated = (prevCount * weight) + currCount

local allowed = 0
if estimated < limit then
  currCount = redis.call('INCR', currKey)
  redis.call('PEXPIRE', currKey, windowMs * 2)
  allowed = 1
end

return { allowed, tostring(math.floor(estimated)) }
`)

// SlidingWindowOptions mirrors { windowMs, limit } from the Node version.
type SlidingWindowOptions struct {
	WindowMs int64
	Limit    float64
}

// SlidingResult mirrors { allowed, estimated }.
type SlidingResult struct {
	Allowed   bool
	Estimated float64
}

// CheckSlidingWindow runs the sliding window check atomically against Redis.
func CheckSlidingWindow(ctx context.Context, rdb *redis.Client, key string, opts SlidingWindowOptions) (SlidingResult, error) {
	now := time.Now().UnixMilli()

	// Same time-slotting trick as the Node version: every caller
	// independently computes the same slot number from the same wall-clock
	// time, so no cross-instance coordination is needed to agree on which
	// window a request belongs to.
	currentSlot := now / opts.WindowMs
	currKey := fmt.Sprintf("%s:%d", key, currentSlot)
	prevKey := fmt.Sprintf("%s:%d", key, currentSlot-1)

	res, err := slidingWindowScript.Run(ctx, rdb, []string{currKey, prevKey},
		opts.WindowMs, opts.Limit, now,
	).Result()
	if err != nil {
		return SlidingResult{}, err
	}

	arr := res.([]interface{})
	allowed := arr[0].(int64) == 1
	estimated, err := parseFloat(arr[1].(string))
	if err != nil {
		return SlidingResult{}, err
	}

	return SlidingResult{Allowed: allowed, Estimated: estimated}, nil
}

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}
