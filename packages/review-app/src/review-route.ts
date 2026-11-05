import type { ApiContext } from "@diffx/api";

/**
 * `/contexts/{id}` pins one snapshot. `/streams/{id}` follows a stream's latest
 * head, resolved on load and navigation only. `/` resolves to a live stream.
 */
export type Route =
  | { kind: "pinned"; id: string }
  | { kind: "live"; stream: string }
  | { kind: "root" };

function routeValue(prefix: string) {
  const match = new RegExp(`^/${prefix}/([^/]+)/?$`).exec(
    window.location.pathname,
  );
  if (!match?.[1]) return "";
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return "";
  }
}

export function routeFromUrl(): Route {
  const id = routeValue("contexts");
  if (id) return { kind: "pinned", id };
  const stream = routeValue("streams");
  return stream ? { kind: "live", stream } : { kind: "root" };
}

export function contextPath(id: string) {
  return `/contexts/${encodeURIComponent(id)}`;
}

export function streamPath(head: ApiContext) {
  return head.streamId
    ? `/streams/${encodeURIComponent(head.streamId)}`
    : contextPath(head.id);
}

/** Scope history never re-resolves a live route's selected snapshot. */
export function sameRoute(left: Route, right: Route) {
  return left.kind === "root"
    ? right.kind === "root"
    : left.kind === "pinned"
      ? right.kind === "pinned" && left.id === right.id
      : right.kind === "live" && left.stream === right.stream;
}
