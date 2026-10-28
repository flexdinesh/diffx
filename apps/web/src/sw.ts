/// <reference lib="webworker" />
// Built as the fixed-name root asset /sw.js (see vite.config.ts) and typed by
// tsconfig.sw.json. Notifications only: no fetch handler and no caching.
import {
  fallbackNotification,
  notificationClickAction,
  readPushPayload,
} from "./push-notification.ts";

declare const self: ServiceWorkerGlobalScope;

self.addEventListener("install", () => {
  void self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(self.clients.claim());
});

function parse(event: PushEvent) {
  try {
    return readPushPayload(event.data?.json());
  } catch {
    return null;
  }
}

self.addEventListener("push", (event) => {
  const payload = parse(event);
  const message = payload ?? fallbackNotification;
  event.waitUntil(
    self.registration.showNotification(message.title, {
      body: message.body,
      data: { url: message.url },
      icon: "/icons/icon-192.png",
      badge: "/icons/badge-96.png",
      ...(payload && { tag: payload.tag, timestamp: payload.timestamp }),
    }),
  );
});

function targetUrl(data: unknown) {
  return typeof data === "object" &&
    data !== null &&
    "url" in data &&
    typeof data.url === "string"
    ? data.url
    : "/";
}

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const target = targetUrl(event.notification.data);
  event.waitUntil(
    self.clients
      .matchAll({ type: "window", includeUncontrolled: true })
      .then((windows) => {
        const decision = notificationClickAction(
          target,
          windows.map((client) => client.url),
          self.location.origin,
        );
        if (decision.action === "focus") {
          const client = windows[decision.index];
          if (client) return client.focus().then(() => undefined);
        }
        return self.clients
          .openWindow(new URL(target, self.location.origin).href)
          .then(() => undefined);
      }),
  );
});
