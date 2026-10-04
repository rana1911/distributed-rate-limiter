package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type endpointCounters struct {
	totalRequests    atomic.Uint64
	allowedRequests  atomic.Uint64
	rejectedRequests atomic.Uint64
	totalLatencyNs   atomic.Uint64
}

type endpointMetric struct {
	Route            string  `json:"route"`
	Algorithm        string  `json:"algorithm"`
	TotalRequests    uint64  `json:"total_requests"`
	AllowedRequests  uint64  `json:"allowed_requests"`
	RejectedRequests uint64  `json:"rejected_requests"`
	TotalLatencyNs   uint64  `json:"total_latency_ns"`
	AverageLatencyMs float64 `json:"average_latency_ms"`
}

type metricsSnapshot struct {
	InstanceID       string           `json:"instance_id"`
	TotalRequests    uint64           `json:"total_requests"`
	AllowedRequests  uint64           `json:"allowed_requests"`
	RejectedRequests uint64           `json:"rejected_requests"`
	TotalLatencyNs   uint64           `json:"total_latency_ns"`
	AverageLatencyMs float64          `json:"average_latency_ms"`
	Routes           []endpointMetric `json:"routes"`
}

type metricsRegistry struct {
	instanceID string
	mu         sync.RWMutex
	endpoints  map[string]*endpointCounters
	labels     map[string]endpointMetric
}

func newMetricsRegistry(instanceID string) *metricsRegistry {
	if instanceID == "" {
		instanceID = "local"
		if hostname, err := os.Hostname(); err == nil && hostname != "" {
			instanceID = hostname
		}
	}

	return &metricsRegistry{
		instanceID: instanceID,
		endpoints:  make(map[string]*endpointCounters),
		labels:     make(map[string]endpointMetric),
	}
}

func (m *metricsRegistry) track(route, algorithm string, next http.Handler) http.Handler {
	m.mu.Lock()
	counters, ok := m.endpoints[route]
	if !ok {
		counters = &endpointCounters{}
		m.endpoints[route] = counters
		m.labels[route] = endpointMetric{Route: route, Algorithm: algorithm}
	}
	m.mu.Unlock()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		counters.totalRequests.Add(1)
		if recorder.statusCode == http.StatusTooManyRequests {
			counters.rejectedRequests.Add(1)
		} else {
			counters.allowedRequests.Add(1)
		}
		counters.totalLatencyNs.Add(uint64(time.Since(started).Nanoseconds()))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusRecorder) WriteHeader(statusCode int) {
	if w.statusCode != 0 {
		return
	}
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusRecorder) Write(body []byte) (int, error) {
	if w.statusCode == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (m *metricsRegistry) snapshot() metricsSnapshot {
	m.mu.RLock()
	endpoints := make([]endpointMetric, 0, len(m.endpoints))
	for route, counters := range m.endpoints {
		metric := m.labels[route]
		metric.TotalRequests = counters.totalRequests.Load()
		metric.AllowedRequests = counters.allowedRequests.Load()
		metric.RejectedRequests = counters.rejectedRequests.Load()
		metric.TotalLatencyNs = counters.totalLatencyNs.Load()
		if metric.TotalRequests > 0 {
			metric.AverageLatencyMs = float64(metric.TotalLatencyNs) /
				float64(metric.TotalRequests) / 1_000_000
		}
		endpoints = append(endpoints, metric)
	}
	m.mu.RUnlock()

	result := metricsSnapshot{
		InstanceID: m.instanceID,
		Routes:     endpoints,
	}
	for _, endpoint := range endpoints {
		result.TotalRequests += endpoint.TotalRequests
		result.AllowedRequests += endpoint.AllowedRequests
		result.RejectedRequests += endpoint.RejectedRequests
		result.TotalLatencyNs += endpoint.TotalLatencyNs
	}
	if result.TotalRequests > 0 {
		result.AverageLatencyMs = float64(result.TotalLatencyNs) /
			float64(result.TotalRequests) / 1_000_000
	}
	return result
}

func (m *metricsRegistry) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(m.snapshot()); err != nil {
		log.Printf("[metrics] failed to encode snapshot: %v", err)
	}
}
