// Package middleware wires the limiter algorithms into standard-library
// net/http handlers. Direct equivalent of src/middleware/rateLimit.js --
// same "fail open on Redis errors" philosophy, same X-RateLimit-Remaining
// header convention, same per-strategy key namespacing.
package middleware

import (
	"log"
	"net/http"
	"strconv"

	"github.com/redis/go-redis/v9"

	"ratelimiter/internal/limiter"
)

// Strategy identifies which algorithm a route should be guarded by.
type Strategy string

const (
	TokenBucket   Strategy = "token-bucket"
	SlidingWindow Strategy = "sliding-window"
)

// KeyFunc decides how to identify the calling client -- equivalent to the
// `keyFn` option in the Node version. Defaults to remote IP if not set.
type KeyFunc func(r *http.Request) string

// Config mirrors the options object passed into rateLimit(...) in Node.
type Config struct {
	Strategy Strategy
	KeyFn    KeyFunc

	// Token bucket settings
	Capacity         float64
	RefillRatePerSec float64

	// Sliding window settings
	WindowMs int64
	Limit    float64
}

// RateLimit returns a net/http middleware -- a function that wraps a
// handler and runs the rate-limit check first, exactly the same shape as
// Express's "middleware(req, res, next)" pattern: in Go, "next" is simply
// the http.Handler passed to the returned function, and calling
// next.ServeHTTP(w, r) is the equivalent of calling next() in Express.
func RateLimit(rdb *redis.Client, cfg Config) func(http.Handler) http.Handler {
	keyFn := cfg.KeyFn
	if keyFn == nil {
		keyFn = func(r *http.Request) string { return r.RemoteAddr }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientKey := "rl:" + string(cfg.Strategy) + ":" + keyFn(r)
			ctx := r.Context()

			var allowed bool
			var remainingHeader string

			switch cfg.Strategy {
			case TokenBucket:
				res, err := limiter.CheckTokenBucket(ctx, rdb, clientKey, limiter.TokenBucketOptions{
					Capacity:         cfg.Capacity,
					RefillRatePerSec: cfg.RefillRatePerSec,
				})
				if err != nil {
					// Fail open: a Redis hiccup shouldn't take the whole
					// API down. Same tradeoff discussion as the Node
					// version -- log loudly, let the request through.
					log.Printf("[rateLimit] backend error, failing open: %v", err)
					next.ServeHTTP(w, r)
					return
				}
				allowed = res.Allowed
				remainingHeader = strconv.FormatInt(int64(res.Remaining), 10)

			case SlidingWindow:
				res, err := limiter.CheckSlidingWindow(ctx, rdb, clientKey, limiter.SlidingWindowOptions{
					WindowMs: cfg.WindowMs,
					Limit:    cfg.Limit,
				})
				if err != nil {
					log.Printf("[rateLimit] backend error, failing open: %v", err)
					next.ServeHTTP(w, r)
					return
				}
				allowed = res.Allowed
				remaining := cfg.Limit - res.Estimated
				if remaining < 0 {
					remaining = 0
				}
				remainingHeader = strconv.FormatInt(int64(remaining), 10)
			}

			w.Header().Set("X-RateLimit-Remaining", remainingHeader)

			if !allowed {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"Too Many Requests","strategy":"` + string(cfg.Strategy) + `"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
