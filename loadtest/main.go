// Equivalent of load-test/run.js -- fires concurrent requests at a target
// URL and reports throughput/latency. Node's version used the autocannon
// library; here we hand-roll it with goroutines, which is idiomatic Go
// and doubles as a small demonstration of Go's concurrency primitives
// (WaitGroup, channels) -- a good thing to be able to talk through in an
// interview alongside the Redis-side atomicity story.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:3000/api/strict", "target URL")
	connections := flag.Int("connections", 50, "concurrent virtual clients")
	duration := flag.Duration("duration", 15*time.Second, "test duration, e.g. 15s")
	flag.Parse()

	fmt.Printf("Load testing %s with %d connections for %s...\n", *url, *connections, *duration)

	var wg sync.WaitGroup
	var total2xx int64
	var totalNon2xx int64
	latencies := make(chan time.Duration, 100_000)
	stop := time.After(*duration)

	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < *connections; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}

				req, err := http.NewRequest("GET", *url, nil)
				if err != nil {
					continue
				}
				// Same as the Node load test: hammer the SAME client key
				// with every request, to specifically test "what happens
				// when one client gets pushed past their limit."
				req.Header.Set("X-API-Key", "load-test-client")

				start := time.Now()
				resp, err := client.Do(req)
				elapsed := time.Since(start)
				if err != nil {
					continue
				}
				resp.Body.Close()

				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					atomic.AddInt64(&total2xx, 1)
				} else {
					atomic.AddInt64(&totalNon2xx, 1)
				}
				latencies <- elapsed
			}
		}()
	}

	wg.Wait()
	close(latencies)

	var samples []time.Duration
	for l := range latencies {
		samples = append(samples, l)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })

	fmt.Println("\n--- Results ---")
	total := total2xx + totalNon2xx
	fmt.Printf("Total requests: %d\n", total)
	fmt.Printf("Requests/sec (avg): %.1f\n", float64(total)/duration.Seconds())
	if len(samples) > 0 {
		fmt.Printf("Latency p50: %v\n", percentile(samples, 50))
		fmt.Printf("Latency p99: %v\n", percentile(samples, 99))
	}
	fmt.Printf("2xx: %d  |  non-2xx (throttled): %d\n", total2xx, totalNon2xx)
}

func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
