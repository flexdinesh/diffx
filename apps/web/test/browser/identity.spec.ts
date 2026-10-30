import { type Page } from "@playwright/test";
import { expect, test } from "./fixtures.ts";

interface RemoteOptions {
  googleEnabled?: boolean;
  email?: string | null;
}
function bootstrap(
  signedIn: boolean,
  admin = false,
  { googleEnabled = false, email = null }: RemoteOptions = {},
) {
  return {
    mode: "remote",
    googleEnabled,
    user: signedIn
      ? { id: "account-a", username: "alice", email, admin }
      : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}
async function remote(
  page: Page,
  signedIn = true,
  admin = false,
  options: RemoteOptions = {},
) {
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({ json: bootstrap(signedIn, admin, options) }),
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
}

test("local mode has no account UI and settings resolves to reviews", async ({
  page,
}) => {
  await page.goto("/settings");
  await expect(page.getByRole("heading", { name: "Piped" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Account settings" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "Account settings" }),
  ).toHaveCount(0);
});

test("the setup link creates the admin once and keeps its secret out of the URL", async ({
  page,
}) => {
  await remote(page, false);
  await page.route("**/api/v2/setup", (route) => {
    if (route.request().method() === "GET")
      return route.fulfill({ json: { required: true } });
    expect(route.request().postDataJSON()).toEqual({
      token: "setup-secret",
      username: "alice",
      password: "a-secure-password",
    });
    return route.fulfill({ json: bootstrap(true, true) });
  });
  await page.goto("/setup#t=setup-secret");
  await expect(
    page.getByRole("heading", { name: "Set up diffx" }),
  ).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
  await page.getByLabel("Username", { exact: true }).fill("alice");
  await page.getByLabel("Password", { exact: true }).fill("a-secure-password");
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/");
  await page.getByRole("button", { name: "Account settings" }).click();
  await expect(page.getByRole("heading", { name: "Password" })).toBeVisible();
  await expect(
    page.getByText("No tokens yet.", { exact: false }),
  ).toBeVisible();
});

test("collector token secret appears once, with owner CSRF and revocation", async ({
  page,
}) => {
  await remote(page);
  const token = {
    id: "token-a",
    name: "Laptop",
    purpose: "collector",
    createdAt: Date.now(),
    lastUsedAt: null,
    expiresAt: Date.now() + 90 * 86_400_000,
  };
  let created = false;
  await page.route("**/api/v2/account/tokens", (route) => {
    if (route.request().method() === "POST") {
      expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
      expect(route.request().postDataJSON()).toEqual({
        name: "Laptop",
        purpose: "collector",
        expiresInDays: 90,
      });
      created = true;
      return route.fulfill({
        status: 201,
        json: { token, secret: "one-time-secret" },
      });
    }
    return route.fulfill({ json: { tokens: created ? [token] : [] } });
  });
  await page.route("**/api/v2/account/tokens/token-a", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    created = false;
    return route.fulfill({ status: 204 });
  });
  await page.goto("/settings");
  await page.getByLabel("Token name").fill("Laptop");
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  const heading = page.getByRole("heading", { name: "Save your token now" });
  const secret = page
    .getByRole("status")
    .filter({ has: heading })
    .locator("pre");
  await expect(secret).toContainText("one-time-secret");
  const configuration: unknown = JSON.parse(await secret.innerText());
  expect(configuration).toEqual({
    remote: { url: new URL(page.url()).origin, token: "one-time-secret" },
  });
  await page.getByRole("button", { name: "Reviews", exact: true }).click();
  await page.getByRole("button", { name: "Account settings" }).click();
  await expect(heading).toHaveCount(0);
  await expect(page.getByText("Laptop", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Revoke Laptop" }).click();
  const confirm = page.getByRole("dialog", { name: "Revoke Laptop?" });
  await expect(confirm.getByRole("button", { name: "Cancel" })).toBeFocused();
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText("Laptop", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Revoke Laptop" }).click();
  await confirm.getByRole("button", { name: "Revoke token" }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText("Laptop", { exact: true })).toHaveCount(0);
});

test("a failed revoke keeps the confirmation open with the error", async ({
  page,
}) => {
  await remote(page);
  const token = {
    id: "token-c",
    name: "Desktop",
    purpose: "api",
    createdAt: Date.now(),
    lastUsedAt: null,
    expiresAt: Date.now() + 90 * 86_400_000,
  };
  await page.route("**/api/v2/account/tokens", (route) =>
    route.fulfill({ json: { tokens: [token] } }),
  );
  await page.route("**/api/v2/account/tokens/token-c", (route) =>
    route.fulfill({ status: 500, json: { detail: "Revocation failed." } }),
  );
  await page.goto("/settings");
  await page.getByRole("button", { name: "Revoke Desktop" }).click();
  const confirm = page.getByRole("dialog", { name: "Revoke Desktop?" });
  await confirm.getByRole("button", { name: "Revoke token" }).click();
  await expect(confirm.getByRole("alert")).toHaveText(/./);
  await expect(confirm).toBeVisible();
  await expect(page.getByText("Desktop", { exact: true })).toBeVisible();
});

test("token form chooses access and expiry from labeled selects", async ({
  page,
}) => {
  await remote(page);
  let body: unknown;
  await page.route("**/api/v2/account/tokens", (route) => {
    if (route.request().method() !== "POST")
      return route.fulfill({ json: { tokens: [] } });
    body = route.request().postDataJSON();
    return route.fulfill({
      status: 201,
      json: {
        token: {
          id: "token-b",
          name: "Agent",
          purpose: "api",
          createdAt: Date.now(),
          lastUsedAt: null,
          expiresAt: Date.now() + 365 * 86_400_000,
        },
        secret: "api-secret",
      },
    });
  });
  await page.goto("/settings");
  await page.getByLabel("Token name").fill("Agent");
  await page.getByLabel("Access", { exact: true }).click();
  await page.getByRole("option", { name: "Review API / MCP" }).click();
  await expect(page.getByLabel("Access", { exact: true })).toContainText(
    "Review API / MCP",
  );
  await expect(
    page.getByText("Read and update your reviews through the API or MCP."),
  ).toBeVisible();
  await page.getByLabel("Expires after", { exact: true }).click();
  await page.getByRole("option", { name: "365 days" }).click();
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  await expect(page.getByRole("button", { name: "Copy token" })).toBeVisible();
  expect(body).toEqual({ name: "Agent", purpose: "api", expiresInDays: 365 });
});

test("GitHub settings stay absent until the server configures the integration", async ({
  page,
}) => {
  await remote(page);
  await page.route("**/api/v2/github/connection", (route) =>
    route.fulfill({
      status: 404,
      json: { detail: "GitHub integration is not configured" },
    }),
  );
  await page.goto("/settings");
  await expect(page.getByRole("heading", { name: "API tokens" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "GitHub" })).toHaveCount(0);
});

test("connecting GitHub starts authorization with CSRF and reports the callback once", async ({
  page,
}) => {
  await remote(page);
  await page.route("**/api/v2/github/connection", (route) => {
    if (route.request().method() === "POST") {
      expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
      return route.fulfill({
        json: { authorizeUrl: "https://github.test/login/oauth/authorize" },
      });
    }
    return route.fulfill({
      json: {
        connected: false,
        installUrl: "https://github.com/apps/diffx/installations/new",
      },
    });
  });
  await page.route("https://github.test/**", (route) =>
    route.fulfill({ contentType: "text/html", body: "<h1>Authorize</h1>" }),
  );
  await page.goto("/settings?github=cancelled");
  await expect(
    page.getByText("GitHub authorization was cancelled."),
  ).toBeVisible();
  expect(new URL(page.url()).search).toBe("");
  await expect(
    page.getByRole("link", { name: "Install on repositories" }),
  ).toHaveAttribute("href", "https://github.com/apps/diffx/installations/new");
  await page
    .getByRole("button", { name: "Connect GitHub", exact: true })
    .click();
  await expect(page.getByRole("heading", { name: "Authorize" })).toBeVisible();
  expect(page.url()).toBe("https://github.test/login/oauth/authorize");
});

test("followed repositories toggle with CSRF, resync, and lost access stays listed", async ({
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
            private: true,
            followed: false,
            lost: false,
          },
          {
            id: 11,
            fullName: "octo/gone",
            private: false,
            followed: true,
            lost: true,
          },
        ],
      },
    }),
  );
  const follows: string[] = [];
  await page.route("**/api/v2/github/follows/*", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    follows.push(
      `${route.request().method()} ${new URL(route.request().url()).pathname}`,
    );
    return route.fulfill({ status: 204 });
  });
  let resynced = false;
  await page.route("**/api/v2/github/resync", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    resynced = true;
    return route.fulfill({ status: 204 });
  });
  await page.goto("/settings");
  await expect(page.getByText("Connected as @alice-gh.")).toBeVisible();
  const app = page.getByRole("checkbox", { name: "octo/app" });
  await expect(app).not.toBeChecked();
  await expect(page.getByRole("img", { name: "Private" })).toBeVisible();
  await expect(page.getByText("Access lost")).toBeVisible();
  await app.click();
  await expect(app).toBeChecked();
  await expect(page.getByText("Following octo/app.")).toBeVisible();
  await page.getByRole("checkbox", { name: "octo/gone" }).click();
  await expect(page.getByRole("checkbox", { name: "octo/gone" })).toHaveCount(
    0,
  );
  expect(follows).toEqual([
    "PUT /api/v2/github/follows/10",
    "DELETE /api/v2/github/follows/11",
  ]);
  await page.getByRole("button", { name: "Resync pull requests" }).click();
  await expect(
    page.getByText("Refreshing pull requests of followed repositories."),
  ).toBeVisible();
  expect(resynced).toBe(true);
});

test("expired GitHub authorization asks to connect again and disconnect confirms", async ({
  page,
}) => {
  await remote(page);
  let connected = true;
  await page.route("**/api/v2/github/connection", (route) => {
    if (route.request().method() === "DELETE") {
      expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
      connected = false;
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({
      json: connected
        ? {
            connected: true,
            login: "alice-gh",
            state: "expired",
            installUrl: "https://github.com/apps/diffx/installations/new",
          }
        : {
            connected: false,
            installUrl: "https://github.com/apps/diffx/installations/new",
          },
    });
  });
  await page.goto("/settings");
  await expect(page.getByText(/expired\. Connect again/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Connect GitHub again" }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Repositories" })).toHaveCount(
    0,
  );
  await page.getByRole("button", { name: "Disconnect", exact: true }).click();
  const confirm = page.getByRole("dialog", { name: "Disconnect GitHub?" });
  await confirm.getByRole("button", { name: "Disconnect GitHub" }).click();
  await expect(confirm).toBeHidden();
  await expect(
    page.getByRole("button", { name: "Connect GitHub", exact: true }),
  ).toBeVisible();
});

test("Google sign-in starts without CSRF and reports the callback outcome once", async ({
  page,
}) => {
  await remote(page, false, false, { googleEnabled: true });
  let started: unknown;
  await page.route("**/api/v2/auth/google", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBeUndefined();
    started = route.request().postDataJSON();
    return route.fulfill({
      json: { authorizeUrl: "https://accounts.google.test/auth?state=s" },
    });
  });
  await page.route("https://accounts.google.test/**", (route) =>
    route.fulfill({ contentType: "text/html", body: "<h1>Google</h1>" }),
  );
  await page.goto("/?signin=no-account");
  await expect(page.getByRole("alert")).toHaveText(
    "No diffx account uses that Google account. Ask us for an invite.",
  );
  expect(new URL(page.url()).search).toBe("");
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Continue with Google" }).click();
  await expect(page.getByRole("heading", { name: "Google" })).toBeVisible();
  expect(started).toEqual({});
  expect(page.url()).toBe("https://accounts.google.test/auth?state=s");
});

test("an invite link shows its email, hides its secret and starts Google sign-in with it", async ({
  page,
}) => {
  await remote(page, false, false, { googleEnabled: true });
  await page.route("**/api/v2/invites/lookup", (route) => {
    expect(route.request().postDataJSON()).toEqual({ token: "invite-secret" });
    return route.fulfill({
      json: { email: "bob@example.com", expiresAt: Date.now() + 86_400_000 },
    });
  });
  let started: unknown;
  await page.route("**/api/v2/invites/google", (route) => {
    started = route.request().postDataJSON();
    return route.fulfill({
      json: { authorizeUrl: "https://accounts.google.test/auth?state=i" },
    });
  });
  await page.route("https://accounts.google.test/**", (route) =>
    route.fulfill({ contentType: "text/html", body: "<h1>Google</h1>" }),
  );
  await page.goto("/invite#t=invite-secret");
  await expect(page.getByRole("heading", { name: "Join diffx" })).toBeVisible();
  await expect(page.getByText("bob@example.com")).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
  expect(page.url()).not.toContain("invite-secret");
  await page.getByRole("button", { name: "Continue with Google" }).click();
  await expect(page.getByRole("heading", { name: "Google" })).toBeVisible();
  expect(started).toEqual({ token: "invite-secret" });
});

test("members see their Google email and cannot open the manage page", async ({
  page,
}) => {
  await remote(page, true, false, { email: "alice@example.com" });
  await page.goto("/settings");
  await expect(
    page.getByText("with Google as alice@example.com", { exact: false }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Manage accounts and invites" }),
  ).toHaveCount(0);
  await page.goto("/manage");
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/");
});

test("the admin invites by email, sees the link once and revokes it", async ({
  page,
}) => {
  await remote(page, true, true, { googleEnabled: true });
  const now = Date.now();
  const pending = {
    id: "invite-a",
    email: "bob@example.com",
    status: "pending",
    createdAt: now,
    expiresAt: now + 48 * 3_600_000,
    usedBy: null,
  };
  let invites: unknown[] = [
    {
      ...pending,
      id: "invite-old",
      email: "carol@example.com",
      status: "used",
      usedBy: "carol",
    },
  ];
  await page.route("**/api/v2/manage/users", (route) =>
    route.fulfill({
      json: {
        users: [
          {
            id: "account-a",
            username: "alice",
            email: null,
            admin: true,
            disabled: false,
            createdAt: now,
          },
        ],
      },
    }),
  );
  await page.route("**/api/v2/manage/invites", (route) => {
    if (route.request().method() === "POST") {
      expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
      expect(route.request().postDataJSON()).toEqual({
        email: "bob@example.com",
      });
      invites = [pending, ...invites];
      return route.fulfill({
        status: 201,
        json: {
          invite: pending,
          url: "https://diffx.example.com/invite#t=one-time",
        },
      });
    }
    return route.fulfill({ json: { invites } });
  });
  await page.route("**/api/v2/manage/invites/invite-a", (route) => {
    expect(route.request().method()).toBe("DELETE");
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    invites = invites.map((invite) =>
      invite === pending ? { ...pending, status: "revoked" } : invite,
    );
    return route.fulfill({ status: 204 });
  });
  await page.goto("/settings");
  await page
    .getByRole("button", { name: "Manage accounts and invites" })
    .click();
  await expect(page.getByRole("heading", { name: "Manage" })).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/manage");
  await expect(page.getByText("Joined as carol")).toBeVisible();
  await expect(page.getByRole("button", { name: "Delete alice" })).toHaveCount(
    0,
  );
  await page.getByLabel("Email", { exact: true }).fill("bob@example.com");
  await page.getByRole("button", { name: "Create invite" }).click();
  await expect(
    page.getByRole("heading", { name: "Send this invite link" }),
  ).toBeVisible();
  await expect(page.locator("pre")).toHaveText(
    "https://diffx.example.com/invite#t=one-time",
  );
  await page
    .getByRole("button", { name: "Revoke invite for bob@example.com" })
    .click();
  const confirm = page.getByRole("dialog", {
    name: "Revoke invite for bob@example.com?",
  });
  await confirm.getByRole("button", { name: "Revoke invite" }).click();
  await expect(confirm).toBeHidden();
  await expect(
    page.getByRole("heading", { name: "Send this invite link" }),
  ).toHaveCount(0);
  await expect(page.getByText("Revoked", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Revoke invite for bob@example.com" }),
  ).toHaveCount(0);
});

test("the admin disables, enables and deletes accounts after confirmation", async ({
  page,
}) => {
  await remote(page, true, true);
  let member = {
    id: "account-b",
    username: "bob",
    email: "bob@example.com",
    admin: false,
    disabled: false,
    createdAt: Date.now(),
  };
  let deleted = false;
  await page.route("**/api/v2/manage/invites", (route) =>
    route.fulfill({ json: { invites: [] } }),
  );
  await page.route("**/api/v2/manage/users", (route) =>
    route.fulfill({
      json: {
        users: [
          {
            id: "account-a",
            username: "alice",
            email: null,
            admin: true,
            disabled: false,
            createdAt: Date.now(),
          },
          ...(deleted ? [] : [member]),
        ],
      },
    }),
  );
  const calls: string[] = [];
  await page.route("**/api/v2/manage/users/account-b**", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    const path = new URL(route.request().url()).pathname;
    calls.push(`${route.request().method()} ${path}`);
    if (path.endsWith("/disable")) member = { ...member, disabled: true };
    else if (path.endsWith("/enable")) member = { ...member, disabled: false };
    else deleted = true;
    return route.fulfill({ status: 204 });
  });
  await page.goto("/manage");
  await expect(
    page.getByText("Invites need Google sign-in.", { exact: false }),
  ).toBeVisible();
  await expect(page.getByLabel("Email", { exact: true })).toHaveCount(0);
  await expect(
    page.getByText("bob@example.com", { exact: false }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Disable bob" }).click();
  const disable = page.getByRole("dialog", { name: "Disable bob?" });
  await expect(disable.getByRole("button", { name: "Cancel" })).toBeFocused();
  await disable.getByRole("button", { name: "Disable account" }).click();
  await expect(disable).toBeHidden();
  await expect(page.getByText("Disabled", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Enable bob" }).click();
  await expect(page.getByRole("button", { name: "Disable bob" })).toBeVisible();
  await page.getByRole("button", { name: "Delete bob" }).click();
  const remove = page.getByRole("dialog", { name: "Delete bob?" });
  await remove.getByRole("button", { name: "Delete account" }).click();
  await expect(remove).toBeHidden();
  await expect(page.getByText("bob@example.com", { exact: false })).toHaveCount(
    0,
  );
  expect(calls).toEqual([
    "POST /api/v2/manage/users/account-b/disable",
    "POST /api/v2/manage/users/account-b/enable",
    "DELETE /api/v2/manage/users/account-b",
  ]);
});

test("another invite link opened in the same tab loads that invite", async ({
  page,
}) => {
  await remote(page, false, false, { googleEnabled: true });
  await page.route("**/api/v2/invites/lookup", (route) => {
    const { token } = route.request().postDataJSON();
    return route.fulfill({
      json: {
        email: token === "second" ? "carol@example.com" : "bob@example.com",
        expiresAt: Date.now() + 86_400_000,
      },
    });
  });
  await page.goto("/invite#t=first");
  await expect(page.getByText("bob@example.com")).toBeVisible();
  await page.evaluate(() => {
    window.location.hash = "t=second";
  });
  await expect(page.getByText("carol@example.com")).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
});
