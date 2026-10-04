// Equivalent of src/server.js -- wires the middleware and both demo
// routes together into a runnable HTTP server.
package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"
	"strings"

	"ratelimiter/internal/middleware"
	"ratelimiter/internal/redisclient"
)

func main() {
	rdb := redisclient.New()
	defer rdb.Close()

	// Sanity-check the Redis connection on startup so failures are loud
	// and immediate rather than surfacing as a mysterious first-request
	// error.
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("warning: could not reach redis at startup: %v", err)
	}

	// Same client-identification logic as the Node version: prefer an
	// X-API-Key header, fall back to remote address.
	keyFn := func(r *http.Request) string {
		if key := r.Header.Get("X-API-Key"); key != "" {
			return key
		}
		if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
			if clientIP := strings.TrimSpace(strings.Split(forwardedFor, ",")[0]); clientIP != "" {
				return clientIP
			}
		}
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return host
		}
		return r.RemoteAddr
	}

	registry := newMetricsRegistry(os.Getenv("INSTANCE_ID"))
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", registry.serveHTTP)

	burstyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "endpoint": "bursty", "strategy": "token-bucket"})
	})
	bursty := middleware.RateLimit(rdb, middleware.Config{
		Strategy:         middleware.TokenBucket,
		KeyFn:            keyFn,
		Capacity:         20,
		RefillRatePerSec: 5,
	})
	mux.Handle("/api/bursty", registry.track(
		"/api/bursty",
		string(middleware.TokenBucket),
		bursty(burstyHandler),
	))

	strictHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "endpoint": "strict", "strategy": "sliding-window"})
	})
	strict := middleware.RateLimit(rdb, middleware.Config{
		Strategy: middleware.SlidingWindow,
		KeyFn:    keyFn,
		WindowMs: 10_000,
		Limit:    10,
	})
	mux.Handle("/api/strict", registry.track(
		"/api/strict",
		string(middleware.SlidingWindow),
		strict(strictHandler),
	))

	// Unprotected health check, same as the Node version.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok"})
	})

	port := getEnv("PORT", "3000")
	log.Printf("rate-limiter demo (go) listening on :%s", port)
	handler := enableCORS(mux)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions {
			if !allowedOrigin(origin) {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func allowedOrigin(origin string) bool {
	configuredOrigin := getEnv("DASHBOARD_ORIGIN", "http://localhost:5173")
	return origin == configuredOrigin || origin == "http://127.0.0.1:5173"
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[http] failed to encode response: %v", err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
