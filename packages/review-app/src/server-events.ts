/** A committed comment, reply or resolution change in one review stream. */
export interface CommentChange {
  contextId: string;
  /** Empty for piped captures, whose context is their stream. */
  streamId: string;
}

export interface ServerEventHandlers {
  /** A context was ingested or deleted; "" means changes may have been missed. */
  ingestion?: (contextId: string) => void;
  comments?: (change: CommentChange) => void;
  /** The stream (re)connected or failed: hints may have been missed. */
  reconnect?: () => void;
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function payload(event: Event) {
  // Read data structurally: test doubles dispatch another realm's MessageEvent.
  const data: unknown = Reflect.get(event, "data");
  if (typeof data !== "string") return null;
  try {
    const value: unknown = JSON.parse(data);
    return record(value) ? value : null;
  } catch {
    return null;
  }
}

function commentChange(event: Event): CommentChange | null {
  const value = payload(event);
  return value &&
    typeof value.contextId === "string" &&
    typeof value.streamId === "string"
    ? { contextId: value.contextId, streamId: value.streamId }
    : null;
}

const subscribers = new Set<ServerEventHandlers>();
let source: EventSource | null = null;

function dispatch(deliver: (handlers: ServerEventHandlers) => void) {
  for (const handlers of [...subscribers]) deliver(handlers);
}

function connect() {
  const events = new EventSource("/api/v2/events");
  const ingestion = (event: Event) => {
    const value = payload(event);
    const contextId =
      value && typeof value.contextId === "string" ? value.contextId : "";
    dispatch((handlers) => handlers.ingestion?.(contextId));
  };
  events.addEventListener("message", ingestion);
  events.addEventListener("ingestion", ingestion);
  events.addEventListener("comments", (event) => {
    const change = commentChange(event);
    // A malformed hint still means something changed; reload as after a gap.
    dispatch((handlers) =>
      change ? handlers.comments?.(change) : handlers.reconnect?.(),
    );
  });
  const reconnect = () => dispatch((handlers) => handlers.reconnect?.());
  events.addEventListener("open", reconnect);
  // EventSource hides HTTP status; a normal API read resolves expired identity.
  events.addEventListener("error", reconnect);
  return events;
}

/**
 * Server-sent change hints share one connection per page: it opens with the
 * first subscriber and closes with the last, so account changes that unmount
 * every owner also end the stream. Hints only prompt reloading durable state.
 */
export function subscribeServerEvents(handlers: ServerEventHandlers) {
  subscribers.add(handlers);
  source ??= connect();
  return () => {
    subscribers.delete(handlers);
    if (subscribers.size || !source) return;
    source.close();
    source = null;
  };
}
