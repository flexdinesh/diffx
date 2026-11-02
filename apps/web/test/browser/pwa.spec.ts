import { type Page } from "@playwright/test";
import { expect, test } from "./fixtures.ts";

const keyBytes = Array.from({ length: 65 }, () => 4);
const publicKey = Buffer.from(keyBytes).toString("base64url");

async function remote(page: Page) {
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
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
      },
    }),
  );
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: [], nextCursor: null } }),
  );
  await page.route("**/api/v2/events", (route) =>
    route.fulfill({ contentType: "text/event-stream", body: "" }),
  );
  await page.route("**/api/v2/account/tokens", (route) =>
    route.fulfill({ json: { tokens: [] } }),
  );
  await page.route("**/api/v2/github/connection", (route) =>
    route.fulfill({ status: 404, json: { detail: "Not found" } }),
  );
  await page.route("**/api/v2/push", (route) =>
    route.fulfill({ json: { enabled: true, publicKey, devices: [] } }),
  );
}

/**
 * Replaces the browser's permission prompt and push service, which headless
 * Chromium cannot reach, keeping the real service worker registration.
 */
async function mockPush(
  page: Page,
  initial: { permission: NotificationPermission; subscribed: boolean },
) {
  await page.addInitScript(
    ({ key, permission, subscribed }) => {
      let state = permission;
      let active = subscribed;
      Object.defineProperty(Notification, "permission", {
        configurable: true,
        get: () => state,
      });
      Object.defineProperty(Notification, "requestPermission", {
        configurable: true,
        value: async () => {
          state = "granted";
          return state;
        },
      });
      const subscription = () => ({
        endpoint: "https://push.example.test/this",
        options: { applicationServerKey: new Uint8Array(key).buffer },
        toJSON: () => ({
          endpoint: "https://push.example.test/this",
          keys: { p256dh: "device-key", auth: "device-auth" },
        }),
        unsubscribe: async () => {
          active = false;
          return true;
        },
      });
      Object.defineProperty(PushManager.prototype, "getSubscription", {
        configurable: true,
        value: async () => (active ? subscription() : null),
      });
      Object.defineProperty(PushManager.prototype, "subscribe", {
        configurable: true,
        value: async () => {
          active = true;
          return subscription();
        },
      });
    },
    { key: keyBytes, ...initial },
  );
}

test("only the remote server links the manifest and registers the service worker", async ({
  page,
}) => {
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Piped" })).toBeVisible();
  await expect(page.locator('link[rel="icon"]')).toHaveAttribute(
    "href",
    "/icons/icon.svg",
  );
  await expect(page.locator('link[rel="manifest"]')).toHaveCount(0);
  await expect(
    page.locator('meta[name="apple-mobile-web-app-title"]'),
  ).toHaveCount(0);
  expect(
    await page.evaluate(async () =>
      (await navigator.serviceWorker.getRegistrations()).map(
        (registration) => registration.scope,
      ),
    ),
  ).toEqual([]);

  const remotePage = await page.context().newPage();
  await remote(remotePage);
  await remotePage.goto("/settings/notifications");
  await expect(
    remotePage.getByRole("heading", { name: "Notifications" }),
  ).toBeVisible();
  await expect(remotePage.locator('link[rel="manifest"]')).toHaveAttribute(
    "href",
    "/manifest.webmanifest",
  );
  await expect(
    remotePage.locator('link[rel="apple-touch-icon"]'),
  ).toHaveAttribute("href", "/icons/apple-touch-icon.png");
  await expect(
    remotePage.locator('meta[name="apple-mobile-web-app-title"]'),
  ).toHaveAttribute("content", "diffx");
  const worker = await remotePage.evaluate(async () => {
    const registration = await navigator.serviceWorker.ready;
    return {
      scope: registration.scope,
      script: registration.active?.scriptURL ?? "",
    };
  });
  expect(new URL(worker.scope).pathname).toBe("/");
  expect(new URL(worker.script).pathname).toBe("/sw.js");
  const manifest: unknown = await (
    await remotePage.request.get("/manifest.webmanifest")
  ).json();
  expect(manifest).toMatchObject({
    name: "diffx",
    start_url: "/",
    scope: "/",
    display: "standalone",
  });
  for (const icon of [
    "/icons/icon-192.png",
    "/icons/icon-512.png",
    "/icons/icon-maskable-512.png",
    "/icons/apple-touch-icon.png",
    "/icons/badge-96.png",
  ])
    expect((await remotePage.request.get(icon)).ok()).toBe(true);
});

test("turning on notifications asks permission on click and registers the device", async ({
  page,
}) => {
  await remote(page);
  await mockPush(page, { permission: "default", subscribed: false });
  const registered: unknown[] = [];
  await page.route("**/api/v2/push/subscriptions", (route) => {
    expect(route.request().method()).toBe("PUT");
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    registered.push(route.request().postDataJSON());
    return route.fulfill({
      json: {
        id: "this",
        label: "Chrome on Linux",
        createdAt: 1,
        lastSuccessAt: null,
      },
    });
  });
  let tested = "";
  await page.route("**/api/v2/push/subscriptions/*/test", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    tested = new URL(route.request().url()).pathname;
    return route.fulfill({ status: 204 });
  });
  await page.goto("/settings/notifications");
  await expect(page.getByText("Get a notification when")).toBeVisible();
  expect(registered).toEqual([]);
  await page.getByRole("button", { name: "Turn on notifications" }).click();
  await expect(
    page.getByText("Notifications are on for this device."),
  ).toBeVisible();
  expect(registered).toEqual([
    {
      endpoint: "https://push.example.test/this",
      keys: { p256dh: "device-key", auth: "device-auth" },
      label: expect.stringMatching(/ on /),
    },
  ]);
  await expect(
    page.getByRole("checkbox", { name: "Send notifications to this device" }),
  ).toBeChecked();
  await page.getByRole("button", { name: "Send test notification" }).click();
  await expect(page.getByText("Test notification sent.")).toBeVisible();
  expect(tested).toBe("/api/v2/push/subscriptions/this/test");
});

test("the browser install prompt becomes an Install app action", async ({
  page,
}) => {
  await remote(page);
  await page.goto("/settings/notifications");
  await expect(
    page.getByRole("heading", { name: "Notifications" }),
  ).toBeVisible();
  await expect(page.getByRole("button", { name: "Install app" })).toHaveCount(
    0,
  );
  await page.evaluate(() => {
    const event = new Event("beforeinstallprompt", { cancelable: true });
    Object.assign(event, {
      prompt: async () => {
        Reflect.set(window, "installPrompted", true);
      },
      userChoice: Promise.resolve({ outcome: "accepted" }),
    });
    window.dispatchEvent(event);
  });
  await page.getByRole("button", { name: "Install app" }).click();
  await expect(page.getByRole("button", { name: "Install app" })).toHaveCount(
    0,
  );
  expect(
    await page.evaluate(() => Reflect.get(window, "installPrompted")),
  ).toBe(true);
});

test("a repository bell mutes notifications and rolls back when refused", async ({
  page,
}) => {
  await remote(page);
  await page.route("**/api/v2/github/connection", (route) =>
    route.fulfill({
      json: {
        connected: true,
        login: "alice-gh",
        state: "active",
        installUrl: "https://github.com/apps/diffx/installations/new",
      },
    }),
  );
  await page.route("**/api/v2/github/repositories", (route) =>
    route.fulfill({
      json: {
        repositories: [
          {
            id: 10,
            fullName: "octo/app",
            private: false,
            followed: true,
            lost: false,
            notify: true,
          },
          {
            id: 11,
            fullName: "octo/docs",
            private: false,
            followed: false,
            lost: false,
            notify: false,
          },
        ],
      },
    }),
  );
  const patches: unknown[] = [];
  let refuse = false;
  await page.route("**/api/v2/github/follows/10", (route) => {
    expect(route.request().method()).toBe("PATCH");
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    patches.push(route.request().postDataJSON());
    return refuse
      ? route.fulfill({ status: 503, json: { detail: "GitHub unavailable" } })
      : route.fulfill({ status: 204 });
  });
  await page.goto("/settings/github");
  const bell = page.getByRole("button", { name: "Notify for octo/app" });
  await expect(bell).toHaveAttribute("aria-pressed", "true");
  await expect(
    page.getByRole("button", { name: "Notify for octo/docs" }),
  ).toHaveCount(0);
  await bell.click();
  await expect(bell).toHaveAttribute("aria-pressed", "false");
  await expect(page.getByText("Notifications off for octo/app.")).toBeVisible();
  refuse = true;
  await bell.click();
  await expect(page.getByRole("alert")).toHaveText("GitHub unavailable");
  await expect(bell).toHaveAttribute("aria-pressed", "false");
  expect(patches).toEqual([{ notify: false }, { notify: true }]);
});

test("signing out removes this device before ending the session", async ({
  page,
}) => {
  await remote(page);
  await mockPush(page, { permission: "granted", subscribed: true });
  const calls: string[] = [];
  await page.route("**/api/v2/push/subscriptions", (route) => {
    calls.push(`${route.request().method()} /api/v2/push/subscriptions`);
    return route.fulfill({
      json: {
        id: "this",
        label: "Chrome on Linux",
        createdAt: 1,
        lastSuccessAt: null,
      },
    });
  });
  await page.route("**/api/v2/push/subscriptions/this", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    calls.push(`${route.request().method()} /api/v2/push/subscriptions/this`);
    return route.fulfill({ status: 204 });
  });
  await page.route("**/api/v2/auth/logout", (route) => {
    calls.push("POST /api/v2/auth/logout");
    return route.fulfill({ status: 204 });
  });
  await page.goto("/settings/notifications");
  await expect(
    page.getByRole("checkbox", { name: "Send notifications to this device" }),
  ).toBeChecked();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  expect(calls.slice(-2)).toEqual([
    "DELETE /api/v2/push/subscriptions/this",
    "POST /api/v2/auth/logout",
  ]);
  expect(
    await page.evaluate(async () => {
      const registration = await navigator.serviceWorker.ready;
      return registration.pushManager.getSubscription();
    }),
  ).toBeNull();
});
