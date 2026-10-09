export const METRICS_URLS = (
  import.meta.env.VITE_METRICS_URLS ??
  "https://rateforge.duckdns.org/metrics/app1,https://rateforge.duckdns.org/metrics/app2"
)
  .split(",")
  .map((url) => url.trim())
  .filter(Boolean);

export const API_URL = import.meta.env.VITE_API_URL ?? "https://rateforge.duckdns.org";

export async function fetchMetrics(url, signal) {
  const response = await fetch(url, { signal });
  if (!response.ok) {
    throw new Error(`HTTP ${response.status}`);
  }

  const data = await response.json();
  if (
    typeof data.instance_id !== "string" ||
    !Number.isFinite(data.total_requests) ||
    !Number.isFinite(data.allowed_requests) ||
    !Number.isFinite(data.rejected_requests) ||
    !Number.isFinite(data.total_latency_ns) ||
    !Array.isArray(data.routes)
  ) {
    throw new Error("The metrics response has an unexpected format");
  }
  if (
    data.routes.some(
      (route) =>
        typeof route.route !== "string" ||
        typeof route.algorithm !== "string" ||
        !Number.isFinite(route.total_requests) ||
        !Number.isFinite(route.allowed_requests) ||
        !Number.isFinite(route.rejected_requests),
    )
  ) {
    throw new Error("The metrics response contains an invalid route entry");
  }

  return data;
}

export function combineMetrics(instances) {
  const available = instances.filter((instance) => instance.metrics);
  const online = available.filter((instance) => !instance.error);
  const combined = online.reduce(
    (total, instance) => {
      total.totalRequests += instance.metrics.total_requests;
      total.allowedRequests += instance.metrics.allowed_requests;
      total.rejectedRequests += instance.metrics.rejected_requests;
      total.totalLatencyNs += instance.metrics.total_latency_ns;
      for (const route of instance.metrics.routes) {
        const key = `${route.route}|${route.algorithm}`;
        const current = total.routes.get(key) ?? {
          route: route.route,
          algorithm: route.algorithm,
          total_requests: 0,
          allowed_requests: 0,
          rejected_requests: 0,
        };
        current.total_requests += route.total_requests;
        current.allowed_requests += route.allowed_requests;
        current.rejected_requests += route.rejected_requests;
        total.routes.set(key, current);
      }
      return total;
    },
    {
      totalRequests: 0,
      allowedRequests: 0,
      rejectedRequests: 0,
      totalLatencyNs: 0,
      routes: new Map(),
    },
  );

  return {
    ...combined,
    averageLatencyMs:
      combined.totalRequests === 0
        ? 0
        : combined.totalLatencyNs / combined.totalRequests / 1_000_000,
    routes: [...combined.routes.values()].sort((a, b) =>
      a.route.localeCompare(b.route),
    ),
    onlineInstances: online.length,
  };
}
