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
import { deferred, json, requests } from "./setup.ts";

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

const preferences = {
  opened: { byMe: false, byOthers: true },
  newCommits: { byMe: false, byOthers: true },
  merged: { byMe: true, byOthers: false },
  closed: { byMe: false, byOthers: false },
  skipDrafts: true,
  onlyAuthors: [],
  neverAuthors: ["dependabot[bot]"],
  baseBranches: ["main", "release/*"],
  mutedTitleTerms: [],
};

interface Server {
  enabled?: boolean;
  github?: "absent" | "disconnected" | "connected";
  /** Whether `GET /api/v2/github/notifications` answers 404. */
  preferencesAbsent?: boolean;
  handle?: (request: Request) => Response | Promise<Response> | undefined;
}

function server({
  enabled = true,
  github = "absent",
  preferencesAbsent = github === "absent",
  handle,
}: Server = {}) {
  const calls: string[] = [];
  const bodies: unknown[] = [];
  requests(async (request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap")
      return json({
        mode: "remote",
        publicPullRequests: false,
        githubEnabled: false,
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
    if (path === "/api/v2/account/agents") return json({ agents: [] });
    if (path === "/api/v2/github/connection")
      return github === "absent"
        ? json({}, 404)
        : json({
            connected: github === "connected",
            ...(github === "connected" && {
              login: "alice-gh",
              state: "active",
            }),
            installUrl:
              "https://github.com/apps/diffx/installations/select_target",
          });
    if (path === "/api/v2/github/repositories")
      return json({ repositories: [] });
    if (path === "/api/v2/github/notifications" && request.method === "GET")
      return preferencesAbsent ? json({}, 404) : json(preferences);
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
    // Stores what it receives, as the server does once normalized.
    if (path === "/api/v2/github/notifications") return json(bodies.at(-1));
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
  server({ github: "connected" });
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

/** The What notifies subsection once its controls render. */
async function whatNotifies() {
  const region = await screen.findByRole("region", { name: "What notifies" });
  await within(region).findByRole("table", {
    name: "Pull request events that notify",
  });
  return region;
}

function checked(region: HTMLElement, name: string) {
  return (
    within(region)
      .getByRole("checkbox", { name })
      .getAttribute("aria-checked") === "true"
  );
}

test("what notifies shows the account's saved preferences", async () => {
  const { calls } = server({ github: "connected" });
  mount();
  const region = await whatNotifies();
  assert.ok(within(region).getByText(/Applies to all your devices/));
  const table = within(region).getByRole("table");
  assert.deepEqual(
    within(table)
      .getAllByRole("columnheader")
      .map((header) => header.textContent),
    ["Event", "By me", "By others"],
  );
  assert.deepEqual(
    within(table)
      .getAllByRole("rowheader")
      .map((header) => header.textContent),
    [
      "OpenedNew, reopened or ready for review",
      "New commits",
      "Merged",
      "Closed without merging",
    ],
  );
  assert.equal(checked(region, "Opened, by me"), false);
  assert.equal(checked(region, "Opened, by others"), true);
  assert.equal(checked(region, "New commits, by others"), true);
  assert.equal(checked(region, "Merged, by me"), true);
  assert.equal(checked(region, "Closed without merging, by others"), false);
  assert.equal(
    checked(region, "Skip drafts until they're ready for review"),
    true,
  );
  const never = within(region).getByRole("textbox", { name: "Never from" });
  assert.equal(never.getAttribute("value"), "dependabot[bot]");
  assert.equal(
    within(region)
      .getByRole("textbox", { name: "Into branches" })
      .getAttribute("value"),
    "main, release/*",
  );
  const only = within(region).getByRole("textbox", { name: "Only from" });
  assert.equal(only.getAttribute("value"), "");
  assert.equal(only.getAttribute("placeholder"), "Anyone");
  assert.match(
    document.getElementById(only.getAttribute("aria-describedby") ?? "")
      ?.textContent ?? "",
    /Applies to others' pull requests/,
  );
  const link = within(region).getByRole("link", { name: "Settings › GitHub" });
  assert.equal(link.getAttribute("href"), "/settings/github");
  // Preferences autosave: no primary button competes with Turn on notifications.
  assert.equal(within(region).queryAllByRole("button").length, 0);
  assert.deepEqual(calls, []);
});

test("toggling an event saves the whole document and announces it", async () => {
  const { calls, bodies } = server({ github: "connected" });
  mount();
  const region = await whatNotifies();
  fireEvent.click(
    within(region).getByRole("checkbox", { name: "Opened, by me" }),
  );
  assert.equal(checked(region, "Opened, by me"), true);
  const saved = await within(region).findByText("Saved");
  assert.equal(saved.getAttribute("role"), "status");
  assert.deepEqual(calls, ["PUT /api/v2/github/notifications"]);
  assert.deepEqual(bodies, [
    { ...preferences, opened: { byMe: true, byOthers: true } },
  ]);

  fireEvent.click(
    within(region).getByRole("checkbox", {
      name: "Skip drafts until they're ready for review",
    }),
  );
  await waitFor(() => assert.equal(bodies.length, 2));
  assert.deepEqual(bodies[1], {
    ...preferences,
    opened: { byMe: true, byOthers: true },
    skipDrafts: false,
  });
});

test("a refused save restores the saved preferences and explains why", async () => {
  const { calls } = server({
    github: "connected",
    handle: (request) =>
      request.url.endsWith("/github/notifications")
        ? json({ detail: "Notification preferences are unavailable." }, 503)
        : undefined,
  });
  mount();
  const region = await whatNotifies();
  fireEvent.click(
    within(region).getByRole("checkbox", { name: "Merged, by me" }),
  );
  assert.equal(checked(region, "Merged, by me"), false);
  assert.equal(
    (await within(region).findByRole("alert")).textContent,
    "Notification preferences are unavailable.",
  );
  assert.equal(checked(region, "Merged, by me"), true);
  assert.equal(within(region).queryByText("Saved"), null);
  assert.deepEqual(calls, ["PUT /api/v2/github/notifications"]);
});

test("a list field saves on Enter and shows the server's normalized value", async () => {
  const { calls, bodies } = server({
    github: "connected",
    handle: (request) =>
      request.url.endsWith("/github/notifications")
        ? json({
            ...preferences,
            neverAuthors: ["dependabot[bot]", "renovate[bot]"],
          })
        : undefined,
  });
  mount();
  const region = await whatNotifies();
  const never = within(region).getByRole("textbox", { name: "Never from" });
  // Unchanged text saves nothing.
  fireEvent.blur(never);
  fireEvent.change(never, { target: { value: "dependabot[bot] " } });
  fireEvent.keyDown(never, { key: "Enter" });
  assert.deepEqual(calls, []);

  fireEvent.change(never, {
    target: { value: " Dependabot[bot] ,, Renovate[bot], dependabot[bot]," },
  });
  fireEvent.keyDown(never, { key: "Enter" });
  await within(region).findByText("Saved");
  assert.deepEqual(calls, ["PUT /api/v2/github/notifications"]);
  assert.deepEqual(bodies, [
    { ...preferences, neverAuthors: ["Dependabot[bot]", "Renovate[bot]"] },
  ]);
  assert.equal(never.getAttribute("value"), "dependabot[bot], renovate[bot]");
});

test("a list over the server's limits shows a field error and saves nothing", async () => {
  const { calls } = server({ github: "connected" });
  mount();
  const region = await whatNotifies();
  const terms = within(region).getByRole("textbox", {
    name: "Mute titles containing",
  });
  const tooMany = Array.from({ length: 21 }, (_, index) => `term${index}`);
  fireEvent.change(terms, { target: { value: tooMany.join(", ") } });
  fireEvent.blur(terms);
  const error = await within(region).findByRole("alert");
  assert.equal(error.textContent, "Enter at most 20, separated by commas.");
  assert.equal(terms.getAttribute("aria-invalid"), "true");
  assert.ok(
    (terms.getAttribute("aria-describedby") ?? "")
      .split(" ")
      .includes(error.id),
  );
  // The text stays for correction.
  assert.equal(terms.getAttribute("value"), tooMany.join(", "));

  fireEvent.change(terms, { target: { value: `chore:, ${"x".repeat(101)}` } });
  assert.equal(terms.getAttribute("aria-invalid"), null);
  fireEvent.keyDown(terms, { key: "Enter" });
  assert.match(
    (await within(region).findByRole("alert")).textContent ?? "",
    /100 characters or fewer/,
  );
  assert.deepEqual(calls, []);
});

test("saves run one at a time and the latest edit wins", async () => {
  const first = deferred<Response>();
  let puts = 0;
  const { bodies } = server({
    github: "connected",
    handle: (request) => {
      if (!request.url.endsWith("/github/notifications")) return undefined;
      puts++;
      return puts === 1 ? first.promise : undefined;
    },
  });
  mount();
  const region = await whatNotifies();
  fireEvent.click(
    within(region).getByRole("checkbox", { name: "Opened, by me" }),
  );
  await waitFor(() => assert.equal(bodies.length, 1));
  fireEvent.click(
    within(region).getByRole("checkbox", { name: "Merged, by others" }),
  );
  fireEvent.click(
    within(region).getByRole("checkbox", {
      name: "Closed without merging, by me",
    }),
  );
  // Nothing else is sent while the first save is in flight.
  assert.equal(bodies.length, 1);
  // The first save answers with only its own edit; newer edits stay.
  first.resolve(json(bodies[0]));
  await waitFor(() => assert.equal(bodies.length, 2));
  assert.equal(checked(region, "Merged, by others"), true);
  assert.equal(checked(region, "Closed without merging, by me"), true);
  await within(region).findByText("Saved");
  assert.deepEqual(bodies[1], {
    ...preferences,
    opened: { byMe: true, byOthers: true },
    merged: { byMe: true, byOthers: true },
    closed: { byMe: true, byOthers: false },
  });
  assert.equal(bodies.length, 2);
  assert.equal(checked(region, "Opened, by me"), true);
  assert.equal(checked(region, "Merged, by others"), true);
  assert.equal(checked(region, "Closed without merging, by me"), true);
});

test("without a GitHub connection what notifies points to connecting", async () => {
  server({ github: "disconnected" });
  mount();
  const region = await screen.findByRole("region", { name: "What notifies" });
  const link = await within(region).findByRole("link", {
    name: "Connect GitHub",
  });
  assert.equal(link.getAttribute("href"), "/settings/github");
  assert.ok(within(region).getByText(/to choose which pull requests notify/));
  assert.equal(within(region).queryByRole("table"), null);
  assert.equal(within(region).queryByRole("checkbox"), null);
  // One pointer to GitHub: connecting comes before following.
  assert.equal(screen.queryByText(/Choose repositories to follow/), null);
});

test("what notifies is absent when the server has no preferences", async () => {
  server({ github: "connected", preferencesAbsent: true });
  mount();
  await screen.findByRole("heading", { name: "This device" });
  // It shows loading until the server answers 404, then leaves.
  await waitFor(() =>
    assert.ok(!screen.queryByRole("heading", { name: "What notifies" })),
  );
  assert.ok(!screen.queryByRole("table"));
});
