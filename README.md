# Distributed Rate Limiter

A distributed rate limiter implemented in Go with Redis-backed token bucket
and sliding-window algorithms. Atomic Redis Lua scripts keep rate-limit
decisions consistent across multiple Go server instances.

## Project structure

```
cmd/server/main.go              # HTTP server entrypoint and routes
cmd/server/metrics.go           # per-instance request metrics
internal/redisclient/           # shared Redis client
internal/limiter/
  tokenbucket.go                 # token bucket algorithm
  slidingwindow.go               # sliding-window algorithm
  limiter_test.go                # Redis-backed integration tests
internal/middleware/ratelimit.go # HTTP rate-limit middleware
loadtest/main.go                 # concurrent load-test client
docker-compose.yml               # Redis, Go instances, Nginx, and dashboard
dashboard/                       # React + Vite live RateForge dashboard
.github/workflows/ci.yml         # Go checks and dashboard build
```

## Rate-limiting algorithms

- **Token bucket (`/api/bursty`)** — capacity of 20 requests, refilling at
  5 requests per second. Allows short bursts while limiting sustained traffic.
- **Sliding window (`/api/strict`)** — allows 10 requests in each 10-second
  window.

Both algorithms use atomic Redis Lua scripts, so concurrent requests handled
by separate Go instances share the same rate-limit state.

## Running it

```bash
# Local (needs Redis running on localhost:6379)
go run ./cmd/server

# Tests
go test ./... -v

# Load test
go run ./loadtest -url http://localhost:3000/api/strict -connections 50 -duration 15s

# Multi-instance API, Redis, Nginx load balancer, and React dashboard
docker compose up --build
```

Docker Compose starts the complete demo stack:

| Service | URL | Purpose |
|---|---|---|
| React dashboard | <http://localhost:5173> | Live metrics, chart, endpoint breakdown, and traffic buttons |
| Nginx load balancer | <http://localhost:3000> | Sends API traffic to either Go instance |
| Go instance 1 | <http://localhost:3001> | Direct API and `/metrics` access |
| Go instance 2 | <http://localhost:3002> | Direct API and `/metrics` access |
| Redis | `localhost:6379` | Shared distributed rate-limit state |

### RateForge dashboard

The dashboard is a React application built with Vite and Recharts. It polls
both Go instances every three seconds and displays combined request totals,
allowed and rejected requests, weighted average latency, recent request volume,
per-instance health, and metrics by route and algorithm. The **Test token
bucket** and **Test sliding window** buttons send requests through Nginx.

The API routes are:

| Route | Algorithm | Limit |
|---|---|---|
| `/api/bursty` | Token bucket | Capacity 20, refill 5 requests/second |
| `/api/strict` | Sliding window | 10 requests per 10 seconds |

To confirm the sliding-window limiter, send 12 requests with the same API key
within 10 seconds. The first 10 should return HTTP 200 and the remaining 2
should return HTTP 429. In PowerShell:

```powershell
$key = "test-$([guid]::NewGuid())"
1..12 | ForEach-Object {
    $status = & curl.exe -s -o NUL -w "%{http_code}" `
        -H "X-API-Key: $key" `
        http://localhost:3000/api/strict
    "{0,2}: HTTP {1}" -f $_, $status
}
```

After a few seconds, the dashboard should show the new totals and rejected
requests. You can also inspect the instance metrics directly:

```text
http://localhost:3001/metrics
http://localhost:3002/metrics
```

Rate-limit state is shared through Redis across the Go instances. Metrics are
in-memory per-instance counters, so they reset when an instance restarts; the
dashboard combines the latest metrics from both instances.

To run the dashboard outside Compose, install its dependencies from the
`dashboard` directory and start Vite:

```bash
cd dashboard
npm ci
npm run dev
```

Set `VITE_METRICS_URLS` to a comma-separated list of metrics URLs and
`VITE_API_URL` to the load-balancer URL if they differ from the local defaults.
The Go service allows browser metrics requests from `http://localhost:5173`
and `http://127.0.0.1:5173`; set `DASHBOARD_ORIGIN` to change the former.
To create a production frontend bundle, run `npm run build` from `dashboard`.

GitHub Actions runs Go formatting checks, tests and vet, then installs and
builds the React dashboard on pushes and pull requests.
