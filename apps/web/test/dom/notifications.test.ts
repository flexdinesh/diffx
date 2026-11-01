import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { AccountSettings } from "../../src/account.tsx";
import { json, requests } from "./setup.ts";

const keyBytes = new Uint8Array(65).fill(4);
const publicKey = Buffer.from(keyBytes).toString("base64url");
const iPhone =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1";

interface Browser {
  permission: NotificationPermission;
  /** What the permission prompt answers. */
  answer: NotificationPermission;
  subscribed: boolean;
  prompts: number;
  unsubscribed: number;
}

const installed: [object, string][] = [];
function define(target: object, name: string, value: unknown) {
  Object.defineProperty(target, name, { configurable: true, value });
  installed.push([target, name]);
}

/** A browser with service workers, Push API and notifications. */
function pushBrowser(initial: Partial<Browser> = {}) {
  const browser: Browser = {
    permission: "default",
    answer: "granted",
    subscribed: false,
    prompts: 0,
    unsubscribed: 0,
    ...initial,
  };
  const subscription = {
    endpoint: "https://push.example.test/this",
    options: { applicationServerKey: keyBytes.slice().buffer },
    toJSON: () => ({
      endpoint: "https://push.example.test/this",
      keys: { p256dh: "device-key", auth: "device-auth" },
    }),
    unsubscribe: async () => {
      browser.subscribed = false;
      browser.unsubscribed++;
      return true;
    },
  };
  const registration = {
    pushManager: {
      getSubscription: async () => (browser.subscribed ? subscription : null),
      subscribe: async () => {
        browser.subscribed = true;
        return subscription;
      },
    },
  };
  define(navigator, "serviceWorker", {
    ready: Promise.resolve(registration),
    getRegistration: async () => registration,
  });
  define(window, "PushManager", class {});
  const notification = {
    get permission() {
      return browser.permission;
    },
    requestPermission: async () => {
      browser.prompts++;
      browser.permission = browser.answer;
      return browser.permission;
    },
  };
  define(window, "Notification", notification);
  define(globalThis, "Notification", notification);
  return browser;
}

afterEach(() => {
  cleanup();
  for (const [target, name] of installed.splice(0))
    Reflect.deleteProperty(target, name);
  window.history.replaceState(null, "", "/");
});

const devices = [
  {
    id: "phone",
    label: "Chrome on Android",
    createdAt: Date.UTC(2026, 8, 2),
    lastSuccessAt: Date.UTC(2026, 9, 27),
  },
  {
    id: "this",
    label: "Chrome on Linux",
    createdAt: Date.UTC(2026, 9, 1),
    lastSuccessAt: null,
  },
];

interface Server {
  enabled?: boolean;
  github?: "absent" | "disconnected";
  handle?: (request: Request) => Response | Promise<Response> | undefined;
}

function server({ enabled = true, github = "absent", handle }: Server = {}) {
  const calls: string[] = [];
  const bodies: unknown[] = [];
  requests(async (request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap")
      return json({
        mode: "remote",
        publicPullRequests: false,
        googleEnabled: false,
        signupEnabled: false,
        passwordSignupEnabled: false,
        user: {
          id: "account-a",
          email: "alice@example.com",
          method: "google",
        },
        csrfToken: "session-csrf",
      });
    if (path === "/api/v2/account/tokens") return json({ tokens: [] });
    if (path === "/api/v2/github/connection")
      return github === "absent"
        ? json({}, 404)
        : json({
            connected: false,
            installUrl: "https://github.com/apps/diffx/installations/new",
          });
    if (path === "/api/v2/push" && request.method === "GET")
      return json(
        enabled
          ? { enabled: true, publicKey, devices }
          : { enabled: false, devices: [] },
      );
    calls.push(`${request.method} ${path}`);
    if (request.method !== "GET")
      assert.equal(request.headers.get("X-CSRF-Token"), "session-csrf");
    if (request.body) bodies.push(await request.json());
    const custom = await handle?.(request);
    if (custom) return custom;
    if (path === "/api/v2/push/subscriptions") return json(devices[1]);
    return new Response(null, { status: 204 });
  });
  return { calls, bodies };
}

function mount() {
  function Content() {
    const { state } = useIdentity();
    return state.status === "ready"
      ? createElement(AccountSettings, { section: "notifications" })
      : null;
  }
  return render(createElement(IdentityProvider, null, createElement(Content)));
}

async function section() {
  await screen.findByRole("heading", { name: "Notifications" });
}

test("server push disabled asks every account to contact us", async () => {
  pushBrowser();
  server({ enabled: false });
  mount();
  await section();
  assert.ok(screen.getByText(/Ask us to turn them on/));
  assert.equal(screen.queryByText(/--public-url/), null);
  assert.equal(screen.queryByRole("heading", { name: "This device" }), null);
});

test("a browser without push explains it and still lists other devices", async () => {
  server();
  mount();
  await section();
  assert.ok(
    screen.getByText("This browser cannot receive push notifications."),
  );
  assert.equal(
    screen.queryByRole("button", { name: "Turn on notifications" }),
    null,
  );
  assert.ok(
    screen.getByRole("button", { name: "Actions for Chrome on Linux" }),
  );
});

test("iPhone Safari explains the Home Screen install before push", async () => {
  define(navigator, "userAgent", iPhone);
  define(navigator, "serviceWorker", {});
  server();
  mount();
  await section();
  assert.ok(screen.getByText(/tap Share, then Add to Home Screen/));
  assert.ok(screen.getByText(/Open diffx from the Home Screen/));
  assert.equal(
    screen.queryByRole("button", { name: "Turn on notifications" }),
    null,
  );
});

test("permission is requested only when turning notifications on", async () => {
  const browser = pushBrowser();
  define(
    navigator,
    "userAgent",
    "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36",
  );
  const { calls, bodies } = server();
  mount();
  await section();
  const turnOn = await screen.findByRole("button", {
    name: "Turn on notifications",
  });
  assert.equal(browser.prompts, 0);
  assert.deepEqual(calls, []);
  fireEvent.click(turnOn);
  await screen.findByText("Notifications are on for this device.");
  assert.equal(browser.prompts, 1);
  assert.equal(browser.subscribed, true);
  assert.deepEqual(calls, ["PUT /api/v2/push/subscriptions"]);
  assert.deepEqual(bodies[0], {
    endpoint: "https://push.example.test/this",
    keys: { p256dh: "device-key", auth: "device-auth" },
    label: "Chrome on Linux",
  });
  const toggle = screen.getByRole("checkbox", {
    name: "Send notifications to this device",
  });
  assert.equal(toggle.getAttribute("aria-checked"), "true");
  assert.ok(screen.getByRole("button", { name: "Send test notification" }));
  // This device leaves the other devices list.
  assert.equal(
    screen.queryByRole("button", { name: "Actions for Chrome on Linux" }),
    null,
  );
});

test("a dismissed permission prompt keeps notifications off without an error", async () => {
  const browser = pushBrowser({ answer: "default" });
  server();
  mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "Turn on notifications" }),
  );
  await screen.findByText("Notifications stay off until you allow them.");
  assert.equal(browser.subscribed, false);
  assert.equal(screen.queryByRole("alert"), null);
});

test("denied permission explains site settings and offers no dead button", async () => {
  pushBrowser({ permission: "denied" });
  server();
  mount();
  await screen.findByText(/Notifications are blocked for this site/);
  assert.equal(
    screen.queryByRole("button", { name: "Turn on notifications" }),
    null,
  );
  assert.equal(
    screen.queryByRole("checkbox", {
      name: "Send notifications to this device",
    }),
    null,
  );
});

test("a subscribed device re-registers, tests and turns off", async () => {
  const browser = pushBrowser({ permission: "granted", subscribed: true });
  let testStatus = 204;
  const { calls } = server({
    handle: (request) =>
      request.url.endsWith("/test")
        ? new Response(
            testStatus === 204 ? null : JSON.stringify({ detail: "x" }),
            { status: testStatus },
          )
        : undefined,
  });
  mount();
  const toggle = await screen.findByRole("checkbox", {
    name: "Send notifications to this device",
  });
  await waitFor(() =>
    assert.equal(toggle.getAttribute("aria-checked"), "true"),
  );
  assert.deepEqual(calls, ["PUT /api/v2/push/subscriptions"]);
  assert.ok(
    screen.getByRole("button", { name: "Actions for Chrome on Android" }),
  );

  fireEvent.click(
    screen.getByRole("button", { name: "Send test notification" }),
  );
  await screen.findByText(/Test notification sent/);
  testStatus = 429;
  fireEvent.click(
    screen.getByRole("button", { name: "Send test notification" }),
  );
  assert.match(
    (await screen.findByRole("alert")).textContent ?? "",
    /Too many test notifications/,
  );
  testStatus = 502;
  fireEvent.click(
    screen.getByRole("button", { name: "Send test notification" }),
  );
  await screen.findByText(/push service did not accept/);

  fireEvent.click(toggle);
  await screen.findByText("Notifications are off for this device.");
  assert.equal(toggle.getAttribute("aria-checked"), "false");
  assert.equal(browser.subscribed, false);
  assert.equal(calls.at(-1), "DELETE /api/v2/push/subscriptions/this");
  assert.equal(
    screen.queryByRole("button", { name: "Send test notification" }),
    null,
  );
});

test("a test for a device the push service dropped turns it off", async () => {
  const browser = pushBrowser({ permission: "granted", subscribed: true });
  server({
    handle: (request) =>
      request.url.endsWith("/test")
        ? json({ detail: "Push subscription not found" }, 404)
        : undefined,
  });
  mount();
  fireEvent.click(
    await screen.findByRole("button", { name: "Send test notification" }),
  );
  assert.match(
    (await screen.findByRole("alert")).textContent ?? "",
    /subscription expired/,
  );
  assert.equal(browser.unsubscribed, 1);
  assert.equal(
    screen
      .getByRole("checkbox", { name: "Send notifications to this device" })
      .getAttribute("aria-checked"),
    "false",
  );
});

test("other devices are removed after confirmation", async () => {
  pushBrowser({ permission: "granted", subscribed: true });
  const { calls } = server();
  mount();
  fireEvent.click(
    await screen.findByRole("button", {
      name: "Actions for Chrome on Android",
    }),
  );
  fireEvent.click(await screen.findByRole("menuitem", { name: "Remove…" }));
  assert.deepEqual(calls, ["PUT /api/v2/push/subscriptions"]);
  fireEvent.click(await screen.findByRole("button", { name: "Remove device" }));
  await screen.findByText("Removed Chrome on Android.");
  assert.equal(calls.at(-1), "DELETE /api/v2/push/subscriptions/phone");
  assert.equal(screen.queryByRole("heading", { name: "Other devices" }), null);
});

test("without followed repositories the section points to GitHub", async () => {
  pushBrowser();
  server({ github: "disconnected" });
  mount();
  const notifications = await screen.findByRole("region", {
    name: "Notifications",
  });
  const link = await within(notifications).findByRole("link", {
    name: "GitHub",
  });
  assert.equal(link.getAttribute("href"), "/settings/github");
  assert.ok(screen.getByText(/Choose repositories to follow under/));
  fireEvent.click(link);
  assert.equal(window.location.pathname, "/settings/github");
});

test("a server without GitHub says nothing can notify yet", async () => {
  pushBrowser();
  server();
  mount();
  await screen.findByText(/GitHub is not set up on this server/);
});

test("an installed app shows its status quietly", async () => {
  pushBrowser();
  define(navigator, "standalone", true);
  server();
  mount();
  await screen.findByText("Installed as an app.");
  assert.equal(screen.queryByRole("button", { name: "Install app" }), null);
});
