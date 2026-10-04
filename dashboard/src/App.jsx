import { useEffect, useMemo, useState } from "react";
import MetricCard from "./components/MetricCard.jsx";
import RequestChart from "./components/RequestChart.jsx";
import { API_URL, combineMetrics, fetchMetrics, METRICS_URLS } from "./api.js";

const POLL_INTERVAL_MS = 3000;

function App() {
  const [instances, setInstances] = useState(() =>
    METRICS_URLS.map((url) => ({ url, metrics: null, error: null })),
  );
  const [history, setHistory] = useState([]);
  const [lastUpdated, setLastUpdated] = useState(null);
  const [requesting, setRequesting] = useState("");
  const [requestResult, setRequestResult] = useState("");

  useEffect(() => {
    let active = true;
    let timer;
    const controller = new AbortController();

    async function refresh() {
      const results = await Promise.allSettled(
        METRICS_URLS.map((url) => fetchMetrics(url, controller.signal)),
      );
      if (!active) return;

      setInstances((previous) => {
        const previousByURL = new Map(previous.map((instance) => [instance.url, instance]));
        return results.map((result, index) => {
          const url = METRICS_URLS[index];
          if (result.status === "fulfilled") {
            return { url, metrics: result.value, error: null };
          }
          return {
            ...(previousByURL.get(url) ?? { url, metrics: null }),
            error: result.reason?.message ?? "Unable to reach the metrics endpoint",
          };
        });
      });
      setLastUpdated(new Date());
      timer = window.setTimeout(refresh, POLL_INTERVAL_MS);
    }

    refresh();
    return () => {
      active = false;
      window.clearTimeout(timer);
      controller.abort();
    };
  }, []);

  const metrics = useMemo(() => combineMetrics(instances), [instances]);

  useEffect(() => {
    if (metrics.onlineInstances === 0) return;
    setHistory((previous) => [
      ...previous.slice(-19),
      {
        time: new Date().toLocaleTimeString([], {
          hour: "2-digit",
          minute: "2-digit",
          second: "2-digit",
        }),
        requests: metrics.totalRequests,
      },
    ]);
  }, [instances, metrics.onlineInstances, metrics.totalRequests]);

  async function sendTestRequest(endpoint) {
    setRequesting(endpoint);
    setRequestResult("");
    try {
      const response = await fetch(`${API_URL}/api/${endpoint}`);
      const message =
        response.status === 429
          ? `${endpoint}: rate limited (429)`
          : `${endpoint}: request accepted (${response.status})`;
      setRequestResult(message);
    } catch (error) {
      setRequestResult(`Request failed: ${error.message}`);
    } finally {
      setRequesting("");
    }
  }

  const hasLoadedMetrics = metrics.onlineInstances > 0;
  const allInstancesOffline = metrics.onlineInstances === 0;
  const hasOfflineInstance = instances.some((instance) => instance.error);

  return (
    <main className="page-shell">
      <header className="topbar">
        <a className="brand" href="#" aria-label="RateForge dashboard home">
          <span className="brand-mark">R</span>
          <span>RateForge</span>
        </a>
        <div className="live-status">
          <span className={`status-dot ${allInstancesOffline ? "offline" : ""}`} />
          {allInstancesOffline
            ? "No instances connected"
            : `${metrics.onlineInstances} of ${instances.length} instances online`}
        </div>
      </header>

      <section className="intro">
        <div>
          <p className="eyebrow">DISTRIBUTED RATE LIMITER</p>
          <h1>Traffic overview</h1>
          <p className="subheading">
            Live request activity across your RateForge instances.
          </p>
        </div>
        <div className="updated">
          <span className="pulse" />
          Auto-refresh · 3 seconds
          <span className="updated-time">
            {lastUpdated ? `Updated ${lastUpdated.toLocaleTimeString()}` : "Connecting…"}
          </span>
        </div>
      </section>

      {allInstancesOffline && (
        <div className="alert error-alert" role="alert">
          <strong>Metrics unavailable.</strong> Check that both Go instances are running
          and reachable on ports 3001 and 3002.
          {instances.map(
            (instance) =>
              instance.error && (
                <div className="instance-error" key={instance.url}>
                  {instance.url}: {instance.error}
                </div>
              ),
          )}
        </div>
      )}
      {hasOfflineInstance && hasLoadedMetrics && (
        <div className="alert warning-alert" role="status">
          Some instance metrics could not be refreshed. Their last snapshots remain
          visible per instance but are excluded from the aggregate.
        </div>
      )}

      {hasLoadedMetrics && (
        <>
          <section className="metric-grid" aria-label="Aggregated rate-limit metrics">
            <MetricCard
              label="Total requests"
              value={metrics.totalRequests.toLocaleString()}
              detail={`Across ${metrics.onlineInstances} live instance${metrics.onlineInstances === 1 ? "" : "s"}`}
            />
            <MetricCard
              label="Allowed"
              value={metrics.allowedRequests.toLocaleString()}
              detail="Requests passed by the limiter"
              tone="green"
            />
            <MetricCard
              label="Rate limited"
              value={metrics.rejectedRequests.toLocaleString()}
              detail="Requests rejected with HTTP 429"
              tone="orange"
            />
            <MetricCard
              label="Average latency"
              value={`${metrics.averageLatencyMs.toFixed(2)} ms`}
              detail="Weighted across all observed requests"
              tone="purple"
            />
          </section>

          <section className="panel traffic-panel">
            <div className="panel-heading">
              <div>
                <h2>Request volume</h2>
                <p>Cumulative requests · last 20 samples</p>
              </div>
              <span className="legend"><i /> Total requests</span>
            </div>
            {history.length > 1 ? (
              <RequestChart data={history} />
            ) : (
              <div className="chart-empty">Waiting for another metrics sample…</div>
            )}
          </section>

          <section className="lower-grid">
            <div className="panel">
              <div className="panel-heading">
                <div>
                  <h2>Instances</h2>
                  <p>Metrics are local to each Go process</p>
                </div>
              </div>
              <div className="instance-list">
                {instances.map((instance, index) => (
                  <article className="instance-row" key={instance.url}>
                    <span className={`status-dot ${instance.error ? "offline" : ""}`} />
                    <div className="instance-info">
                      <strong>
                        {instance.metrics?.instance_id ?? `RateForge ${index + 1}`}
                      </strong>
                      <span>{instance.url}</span>
                      {instance.error && <span className="instance-error">{instance.error}</span>}
                    </div>
                    <div className="instance-count">
                      {instance.metrics
                        ? `${instance.metrics.total_requests.toLocaleString()} req`
                        : "—"}
                    </div>
                  </article>
                ))}
              </div>
            </div>

            <div className="panel">
              <div className="panel-heading">
                <div>
                  <h2>Generate traffic</h2>
                  <p>Send a test request through the load balancer</p>
                </div>
              </div>
              <div className="test-actions">
                <button
                  type="button"
                  onClick={() => sendTestRequest("bursty")}
                  disabled={Boolean(requesting)}
                >
                  {requesting === "bursty" ? "Sending…" : "Test token bucket"}
                </button>
                <button
                  type="button"
                  className="secondary"
                  onClick={() => sendTestRequest("strict")}
                  disabled={Boolean(requesting)}
                >
                  {requesting === "strict" ? "Sending…" : "Test sliding window"}
                </button>
              </div>
              <p className="request-result" role="status">
                {requestResult || "Requests are routed to either server instance."}
              </p>
            </div>
          </section>

          <section className="panel routes-panel">
            <div className="panel-heading">
              <div>
                <h2>Endpoint breakdown</h2>
                <p>Combined route metrics from responding instances</p>
              </div>
            </div>
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Endpoint</th>
                    <th>Algorithm</th>
                    <th>Requests</th>
                    <th>Allowed</th>
                    <th>Rejected</th>
                  </tr>
                </thead>
                <tbody>
                  {metrics.routes.map((route) => (
                    <tr key={`${route.route}-${route.algorithm}`}>
                      <td className="route-name">{route.route}</td>
                      <td><span className="algorithm-tag">{route.algorithm}</span></td>
                      <td>{route.total_requests.toLocaleString()}</td>
                      <td>{route.allowed_requests.toLocaleString()}</td>
                      <td className="rejected-cell">{route.rejected_requests.toLocaleString()}</td>
                    </tr>
                  ))}
                  {metrics.routes.length === 0 && (
                    <tr><td className="empty-row" colSpan="5">No rate-limited requests yet.</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </section>
        </>
      )}

      <footer>
        <span>RateForge</span>
        <span>Rate-limit state lives in Redis · Metrics are per instance</span>
      </footer>
    </main>
  );
}

export default App;
