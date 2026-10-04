package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetricsTrackRateLimitOutcomesByRoute(t *testing.T) {
	registry := newMetricsRegistry("test-instance")
	handler := registry.track("/api/strict", "sliding-window", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limited") == "true" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/strict", "/api/strict?limited=true"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	snapshot := registry.snapshot()
	if snapshot.InstanceID != "test-instance" {
		t.Fatalf("instance id = %q, want test-instance", snapshot.InstanceID)
	}
	if snapshot.TotalRequests != 2 || snapshot.AllowedRequests != 1 || snapshot.RejectedRequests != 1 {
		t.Fatalf("unexpected aggregate metrics: %+v", snapshot)
	}
	if len(snapshot.Routes) != 1 || snapshot.Routes[0].Route != "/api/strict" ||
		snapshot.Routes[0].Algorithm != "sliding-window" {
		t.Fatalf("unexpected route metrics: %+v", snapshot.Routes)
	}
	if snapshot.TotalLatencyNs == 0 {
		t.Fatal("expected recorded latency to be positive")
	}
}

func TestMetricsEndpointReturnsAggregatedSnapshot(t *testing.T) {
	registry := newMetricsRegistry("test-instance")
	handler := registry.track("/api/bursty", "token-bucket", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/bursty", nil))

	response := httptest.NewRecorder()
	registry.serveHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}

	var snapshot metricsSnapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if snapshot.TotalRequests != 1 || snapshot.AllowedRequests != 1 ||
		len(snapshot.Routes) != 1 || snapshot.Routes[0].Algorithm != "token-bucket" {
		t.Fatalf("unexpected metrics response: %+v", snapshot)
	}
}

func TestMetricsEndpointRejectsNonGetMethods(t *testing.T) {
	registry := newMetricsRegistry("test-instance")
	response := httptest.NewRecorder()
	registry.serveHTTP(response, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestMetricsSnapshotComputesWeightedAverageLatency(t *testing.T) {
	registry := newMetricsRegistry("test-instance")
	registry.track("/api/strict", "sliding-window", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	counters := registry.endpoints["/api/strict"]
	counters.totalRequests.Store(4)
	counters.totalLatencyNs.Store(20_000_000)

	snapshot := registry.snapshot()
	if snapshot.AverageLatencyMs != 5 {
		t.Fatalf("average latency = %v ms, want 5 ms", snapshot.AverageLatencyMs)
	}
	if snapshot.Routes[0].AverageLatencyMs != 5 {
		t.Fatalf("route average latency = %v ms, want 5 ms", snapshot.Routes[0].AverageLatencyMs)
	}
}

func TestEnableCORSRestrictsOrigins(t *testing.T) {
	t.Setenv("DASHBOARD_ORIGIN", "http://localhost:5173")
	handler := enableCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	allowed := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodOptions, "/metrics", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	handler.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("allowed preflight status = %d, want %d", allowed.Code, http.StatusNoContent)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allowed origin header = %q", got)
	}

	blocked := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodOptions, "/metrics", nil)
	request.Header.Set("Origin", "https://attacker.example")
	handler.ServeHTTP(blocked, request)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("blocked preflight status = %d, want %d", blocked.Code, http.StatusForbidden)
	}
}
