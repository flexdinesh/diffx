import { type Page } from "@playwright/test";
import { expect, test } from "./fixtures.ts";

function bootstrap(signedIn: boolean, admin = false) {
  return {
    mode: "remote",
    googleEnabled: false,
    user: signedIn ? { id: "account-a", username: "alice", admin } : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}
async function remote(page: Page, signedIn = true, admin = false) {
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({ json: bootstrap(signedIn, admin) }),
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

test("bootstrap failure stays an error and retries without inferring local mode", async ({
  page,
}) => {
  let failed = true;
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill(
      failed
        ? { status: 503, json: { detail: "Identity unavailable" } }
        : { json: bootstrap(false) },
    ),
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Cannot load diffx" }),
  ).toBeVisible();
  await expect(page.getByText("Identity unavailable")).toBeVisible();
  failed = false;
  await page.getByRole("button", { name: "Retry" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});

test("members get no password form; invalid admin login preserves recovery", async ({
  page,
}) => {
  await remote(page, false);
  await page.route("**/api/v2/auth/login", (route) =>
    route.fulfill({
      status: 401,
      json: { detail: "Authentication required" },
    }),
  );
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
  await page.goto("/admin/login");
  await page.getByLabel("Username", { exact: true }).fill("alice");
  await page.getByLabel("Password", { exact: true }).fill("incorrect-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Authentication required");
  await expect(page.getByLabel("Username", { exact: true })).toHaveValue(
    "alice",
  );
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
  await page.getByRole("button", { name: "Create admin", exact: true }).click();
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

test("a completed setup points to admin sign in", async ({ page }) => {
  await remote(page, false);
  await page.route("**/api/v2/setup", (route) =>
    route.fulfill({ json: { required: false } }),
  );
  await page.goto("/setup#t=stale-secret");
  await expect(
    page.getByRole("link", { name: "Sign in as admin" }),
  ).toHaveAttribute("href", "/admin/login");
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
});

test("member settings have no password section", async ({ page }) => {
  await remote(page);
  await page.goto("/settings");
  await expect(page.getByRole("heading", { name: "API tokens" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Password" })).toHaveCount(0);
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

test("token list load failure retries with the shared Retry action", async ({
  page,
}) => {
  await remote(page);
  let failed = true;
  await page.route("**/api/v2/account/tokens", (route) =>
    route.fulfill(
      failed
        ? { status: 503, json: { detail: "Tokens unavailable" } }
        : { json: { tokens: [] } },
    ),
  );
  await page.goto("/settings");
  await expect(page.getByRole("alert")).toContainText("Tokens unavailable");
  await expect(page.getByLabel("Token name")).toHaveCount(0);
  failed = false;
  await page.getByRole("button", { name: "Retry", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveCount(0);
  await expect(page.getByLabel("Token name")).toBeVisible();
});

test("expired account requests remove workspace identity and return to sign in", async ({
  page,
}) => {
  await remote(page);
  await page.route("**/api/v2/account/tokens", (route) =>
    route.fulfill({ status: 401, json: { detail: "Session expired" } }),
  );
  await page.goto("/settings");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Account settings" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Account settings" }),
  ).toHaveCount(0);
});

test("password change sends CSRF and signs out after revoking browser sessions", async ({
  page,
}) => {
  await remote(page, true, true);
  await page.route("**/api/v2/account/password", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    expect(route.request().postDataJSON()).toEqual({
      currentPassword: "old-password-123",
      newPassword: "new-password-456",
    });
    return route.fulfill({ status: 204 });
  });
  await page.goto("/settings");
  await page
    .getByLabel("Current password", { exact: true })
    .fill("old-password-123");
  await page
    .getByLabel("New password", { exact: true })
    .fill("new-password-456");
  await page
    .getByRole("button", { name: "Change password and sign out" })
    .click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
});

test("logout failure stays recoverable and success disposes account UI", async ({
  page,
}) => {
  await remote(page);
  let failed = true;
  await page.route("**/api/v2/auth/logout", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBe("session-csrf");
    return route.fulfill(
      failed
        ? { status: 503, json: { detail: "Logout unavailable" } }
        : { status: 204 },
    );
  });
  await page.goto("/settings");
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText("Logout unavailable");
  failed = false;
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
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
