/**
 * Push notification rules shared by the service worker and its tests. Free of
 * DOM and worker globals so both TypeScript programs compile it.
 */

/** A decrypted push message from the diffx server. */
export interface PushPayload {
  kind: "pull_request" | "test";
  title: string;
  body: string;
  /** Same-origin path, `/streams/<id>` or `/settings/notifications`. */
  url: string;
  tag: string;
  timestamp: number;
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/** Validates a parsed push message; anything else is not a diffx payload. */
export function readPushPayload(value: unknown): PushPayload | null {
  if (!record(value)) return null;
  const { kind, title, body, url, tag, timestamp } = value;
  if (
    (kind !== "pull_request" && kind !== "test") ||
    typeof title !== "string" ||
    title === "" ||
    typeof body !== "string" ||
    typeof url !== "string" ||
    !url.startsWith("/") ||
    url.startsWith("//") ||
    typeof tag !== "string" ||
    typeof timestamp !== "number" ||
    !Number.isFinite(timestamp)
  )
    return null;
  return { kind, title, body, url, tag, timestamp };
}

/** Shown for a push the worker cannot read: browsers penalize silent pushes. */
export const fallbackNotification = {
  title: "New pull request diff",
  body: "",
  url: "/",
};

export type NotificationClickAction =
  | { action: "focus"; index: number }
  | { action: "open"; url: string };

/**
 * Focuses a window already showing the target page, or opens a new one. An
 * existing window is never navigated, so its unsaved comment drafts survive.
 */
export function notificationClickAction(
  target: string,
  windowUrls: readonly string[],
  origin: string,
): NotificationClickAction {
  const url = new URL(target, origin);
  const index = windowUrls.findIndex((value) => {
    const candidate = new URL(value, origin);
    return (
      candidate.origin === url.origin && candidate.pathname === url.pathname
    );
  });
  return index === -1
    ? { action: "open", url: url.href }
    : { action: "focus", index };
}
