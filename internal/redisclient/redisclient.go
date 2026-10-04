// Package redisclient provides a single shared Redis connection used by
// every limiter implementation. Equivalent role to src/redisClient.js in
// the Node version: one connection pool, created once, imported everywhere
// else instead of every file opening its own connection.
package redisclient

import (
	"os"

	"github.com/redis/go-redis/v9"
)

// New creates a Redis client configured from environment variables,
// falling back to sensible local defaults -- same pattern as the Node
// version's REDIS_HOST / REDIS_PORT env vars, so the same binary works
// unmodified whether it's run on your laptop or inside Docker Compose
// (where REDIS_HOST=redis resolves via Compose's internal DNS).
func New() *redis.Client {
	host := getEnv("REDIS_HOST", "localhost")
	port := getEnv("REDIS_PORT", "6379")

	return redis.NewClient(&redis.Options{
		Addr:       host + ":" + port,
		MaxRetries: 3,
	})
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
