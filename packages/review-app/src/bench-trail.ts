import { saveSessionValue, sessionValue } from "./preferences.ts";

/**
 * The Review bench's last URL and row order in this tab: "Reviews" returns to
 * those filters, and the review stepper follows that order.
 */
export type BenchTrail = { path: string; order: string[] };

const key = "bench-trail";

export function benchTrail(): BenchTrail {
  try {
    const data: unknown = JSON.parse(sessionValue(key) ?? "null");
    if (typeof data === "object" && data !== null && !Array.isArray(data)) {
      const path = "path" in data ? data.path : undefined;
      const order = "order" in data ? data.order : undefined;
      if (
        typeof path === "string" &&
        path.startsWith("/") &&
        Array.isArray(order)
      )
        return {
          path,
          order: order.filter((value) => typeof value === "string"),
        };
    }
  } catch {
    // A malformed trail falls back to the default bench.
  }
  return { path: "/", order: [] };
}

export function saveBenchTrail(trail: BenchTrail) {
  saveSessionValue(key, JSON.stringify(trail));
}
