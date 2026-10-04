package limiter

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// These are integration tests -- same philosophy as the Node version's
// test/limiters.test.js: they run against a real Redis instance rather
// than mocking it, because the whole point of this project is the
// interaction with Redis (the atomic Lua scripts), and mocking that away
// would miss the exact bug (race conditions) this project solves.

func testRedisClient() *redis.Client {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	return redis.NewClient(&redis.Options{Addr: addr})
}

func TestTokenBucket_AllowsUpToCapacityThenBlocks(t *testing.T) {
	rdb := testRedisClient()
	defer rdb.Close()
	ctx := context.Background()

	key := fmt.Sprintf("test:tb:%d", time.Now().UnixNano())
	opts := TokenBucketOptions{Capacity: 3, RefillRatePerSec: 0.001}

	for i := 0; i < 3; i++ {
		res, err := CheckTokenBucket(ctx, rdb, key, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !res.Allowed {
			t.Fatalf("request %d: expected allowed, got blocked", i+1)
		}
	}

	res, err := CheckTokenBucket(ctx, rdb, key, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Allowed {
		t.Fatal("expected 4th request to be blocked once capacity is exhausted")
	}
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	rdb := testRedisClient()
	defer rdb.Close()
	ctx := context.Background()

	key := fmt.Sprintf("test:tb-refill:%d", time.Now().UnixNano())
	opts := TokenBucketOptions{Capacity: 1, RefillRatePerSec: 10}

	r1, _ := CheckTokenBucket(ctx, rdb, key, opts)
	r2, _ := CheckTokenBucket(ctx, rdb, key, opts)
	if !r1.Allowed {
		t.Fatal("expected first request to be allowed")
	}
	if r2.Allowed {
		t.Fatal("expected second request to be blocked (bucket empty)")
	}

	time.Sleep(150 * time.Millisecond)

	r3, err := CheckTokenBucket(ctx, rdb, key, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r3.Allowed {
		t.Fatal("expected request to be allowed again after refill window")
	}
}

func TestSlidingWindow_BlocksAfterLimit(t *testing.T) {
	rdb := testRedisClient()
	defer rdb.Close()
	ctx := context.Background()

	key := fmt.Sprintf("test:sw:%d", time.Now().UnixNano())
	opts := SlidingWindowOptions{WindowMs: 60_000, Limit: 3}

	allowedCount := 0
	for i := 0; i < 5; i++ {
		res, err := CheckSlidingWindow(ctx, rdb, key, opts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Allowed {
			allowedCount++
		}
	}

	if allowedCount != 3 {
		t.Fatalf("expected exactly 3 allowed requests within limit, got %d", allowedCount)
	}
}
