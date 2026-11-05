import assert from "node:assert/strict";
import test from "node:test";
import {
  fallbackNotification,
  notificationClickAction,
  readPushPayload,
} from "../src/push-notification.ts";
import {
  applicationServerKey,
  appleMobile,
  deviceLabel,
  pushSupport,
} from "../src/push.ts";

const origin = "https://diffx.example.com";

test("a notification click focuses the window already on its page", () => {
  assert.deepEqual(
    notificationClickAction(
      "/streams/s1",
      [`${origin}/settings`, `${origin}/streams/s1?scope=staged#file`],
      origin,
    ),
    { action: "focus", index: 1 },
  );
});

test("a notification click opens a window instead of navigating another", () => {
  assert.deepEqual(
    notificationClickAction(
      "/streams/s2",
      [
        `${origin}/streams/s1`,
        `${origin}/`,
        "https://other.example/streams/s2",
      ],
      origin,
    ),
    { action: "open", url: `${origin}/streams/s2` },
  );
  assert.deepEqual(notificationClickAction("/", [], origin), {
    action: "open",
    url: `${origin}/`,
  });
});

test("push payloads are read only in the server's shape", () => {
  const payload = {
    kind: "pull_request",
    title: "octo/app #12",
    body: "Add login",
    url: "/streams/s1",
    tag: "pr-12",
    timestamp: 1_790_000_000_000,
  };
  assert.deepEqual(readPushPayload(payload), payload);
  assert.deepEqual(readPushPayload({ ...payload, kind: "test", extra: 1 }), {
    ...payload,
    kind: "test",
  });
  for (const invalid of [
    null,
    "text",
    { ...payload, kind: "merge" },
    { ...payload, title: "" },
    { ...payload, url: "https://evil.example/" },
    { ...payload, url: "//evil.example/" },
    { ...payload, timestamp: "now" },
    { ...payload, tag: undefined },
  ])
    assert.equal(readPushPayload(invalid), null);
  assert.equal(fallbackNotification.url, "/");
});

test("device labels name the browser and system", () => {
  const cases: [string, number, string][] = [
    [
      "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36",
      5,
      "Chrome on Android",
    ],
    [
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
      5,
      "Safari on iPhone",
    ],
    // A Home Screen app omits Safari's token.
    [
      "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148",
      5,
      "Safari on iPhone",
    ],
    // iPadOS reports a desktop Mac with touch.
    [
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
      5,
      "Safari on iPad",
    ],
    [
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
      0,
      "Safari on macOS",
    ],
    [
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0",
      0,
      "Edge on Windows",
    ],
    [
      "Mozilla/5.0 (X11; Linux x86_64; rv:133.0) Gecko/20100101 Firefox/133.0",
      0,
      "Firefox on Linux",
    ],
    ["curl/8.0", 0, "Web browser"],
  ];
  for (const [userAgent, touchPoints, label] of cases)
    assert.equal(deviceLabel(userAgent, touchPoints), label, userAgent);
});

test("iOS Safari tabs need the Home Screen app before push", () => {
  const supported = {
    serviceWorker: true,
    pushManager: true,
    notification: true,
    appleMobile: false,
    standalone: false,
  };
  assert.equal(pushSupport(supported), "supported");
  assert.equal(
    pushSupport({ ...supported, appleMobile: true, standalone: true }),
    "supported",
  );
  const safariTab = {
    ...supported,
    pushManager: false,
    notification: false,
    appleMobile: true,
  };
  assert.equal(pushSupport(safariTab), "ios-install");
  // Installed but still without push: an iOS release before Web Push.
  assert.equal(pushSupport({ ...safariTab, standalone: true }), "unsupported");
  assert.equal(
    pushSupport({ ...supported, pushManager: false }),
    "unsupported",
  );
  assert.equal(appleMobile("Mozilla/5.0 (iPad; CPU OS 17_0)", 0), true);
  assert.equal(appleMobile("Mozilla/5.0 (Macintosh)", 0), false);
});

test("the VAPID key decodes from base64url", () => {
  const bytes = Uint8Array.from({ length: 65 }, (_, index) => index * 3);
  const encoded = Buffer.from(bytes).toString("base64url");
  assert.deepEqual(applicationServerKey(encoded), bytes);
});
