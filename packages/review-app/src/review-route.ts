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

/** The absolute live link to a review, which comment permalinks extend. */
export function reviewUrl(head: ApiContext) {
  return new URL(streamPath(head), window.location.origin).href;
}

/** `?comment={id}` opens a review at one comment. */
export function commentFromUrl() {
  return new URLSearchParams(window.location.search).get("comment") ?? "";
}

/** Consumes a handled permalink so scope history and reloads do not repeat it. */
export function dropCommentFromUrl() {
  const url = new URL(window.location.href);
  if (!url.searchParams.has("comment")) return;
  url.searchParams.delete("comment");
  window.history.replaceState(window.history.state, "", url);
}

/** Scope history never re-resolves a live route's selected snapshot. */
export function sameRoute(left: Route, right: Route) {
  return left.kind === "root"
    ? right.kind === "root"
    : left.kind === "pinned"
      ? right.kind === "pinned" && left.id === right.id
      : right.kind === "live" && left.stream === right.stream;
}
