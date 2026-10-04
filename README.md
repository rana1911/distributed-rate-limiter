# Distributed Rate Limiter (Go)

A direct Go port of the [Node.js distributed rate limiter](../distributed-rate-limiter) --
same architecture, same two algorithms (token bucket + sliding window),
same atomic Redis Lua scripts, same multi-instance Docker Compose proof.
This version exists to demonstrate the same concepts in a language more
commonly used for infra/systems tooling in production.

## What's identical to the Node version

- The **Lua scripts themselves** are byte-for-byte the same logic --
  algorithm correctness doesn't change based on host language.
- The **atomicity story**: the whole read-modify-write cycle still runs
  as one uninterruptible operation inside Redis, for the same race-
  condition reasons explained in the Node version's docs.
- The **multi-instance proof**: `docker compose up --build` starts two
  independent server processes sharing one Redis, exactly like the Node
  version, to prove cross-instance correctness.

## What's different (and why)

| | Node version | Go version |
|---|---|---|
| HTTP framework | Express | standard library `net/http` |
| Redis client | `ioredis` | `go-redis/v9` |
| Script execution | manual `SCRIPT LOAD` / `EVALSHA` + NOSCRIPT retry | `redis.NewScript(...).Run(...)` handles EVALSHA/EVAL fallback automatically |
| Middleware shape | `(req, res, next) => {}` | `func(http.Handler) http.Handler` -- functions wrapping handlers |
| Tests | `node:test` | Go's built-in `testing` package |
| Load test | `autocannon` (npm package) | hand-rolled with goroutines + a `sync.WaitGroup` (see `loadtest/main.go`) -- a good opportunity to talk through Go's concurrency primitives in an interview alongside the Redis-side atomicity |
| Concurrency model | single-threaded event loop, async I/O | real OS threads via goroutines, explicit `sync`/`atomic` where shared state is touched |

That concurrency-model difference is worth understanding for interviews:
Node handles many simultaneous requests on one thread via its event
loop -- there's never *within-process* concurrent execution of your JS,
so the only race condition that matters is the *distributed* one across
separate processes (which is what the Lua script fixes). Go's goroutines
genuinely run concurrently, so a Go rate limiter has to be race-safe both
**within** a single process (which `go-redis`'s client handles safely
under the hood, and our own code doesn't touch any shared mutable state
outside of Redis) and **across** processes (same Lua-script fix as
before). Being able to articulate that distinction is a good sign of
understanding, not just memorized code.

## Project structure

```
cmd/server/main.go              # entrypoint, wires routes + middleware (~ src/server.js)
internal/redisclient/           # shared Redis client (~ src/redisClient.js)
internal/limiter/
  tokenbucket.go                 # ~ src/limiters/tokenBucket.js
  slidingwindow.go               # ~ src/limiters/slidingWindow.js
  limiter_test.go                # ~ test/limiters.test.js
internal/middleware/ratelimit.go # ~ src/middleware/rateLimit.js
loadtest/main.go                # ~ load-test/run.js
docker-compose.yml               # Redis + 2 app instances, same as Node version
dashboard/                       # React + Vite live RateForge dashboard
.github/workflows/ci.yml         # Go checks and dashboard build
```

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

## A note on module fetching in restricted network environments

This project was originally built in a sandboxed environment without
access to the public Go module proxy (`proxy.golang.org`). If you hit
similar restrictions, `GOPROXY=direct GOSUMDB=off go get <module>` fetches
directly from the module's source repo (e.g. GitHub) via Git instead of
going through the proxy/checksum-database layer. In a normal development
environment with full internet access, you don't need either of those
env vars -- the default `GOPROXY` setup just works.
