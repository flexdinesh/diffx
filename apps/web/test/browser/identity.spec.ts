import { type Page } from "@playwright/test";
import { expect, test } from "./fixtures.ts";

interface RemoteOptions {
  googleEnabled?: boolean;
  signupEnabled?: boolean;
  passwordSignupEnabled?: boolean;
  email?: string;
  method?: "google" | "password";
}
function bootstrap(
  signedIn: boolean,
  {
    googleEnabled = false,
    signupEnabled = false,
    passwordSignupEnabled = false,
    email = "alice@example.com",
    method = "google",
  }: RemoteOptions = {},
) {
  return {
    mode: "remote",
    publicPullRequests: false,
    googleEnabled,
    signupEnabled,
    passwordSignupEnabled,
    user: signedIn ? { id: "account-a", email, method } : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}
async function remote(
  page: Page,
  signedIn = true,
  options: RemoteOptions = {},
) {
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({ json: bootstrap(signedIn, options) }),
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

test("open signup creates a password account with a password section", async ({
  page,
}) => {
  const open = { signupEnabled: true, passwordSignupEnabled: true };
  const email = "alice.with.a.long.email.address@example.com";
  await remote(page, false, open);
  await page.route("**/api/v2/auth/signup", (route) => {
    expect(route.request().headers()["x-csrf-token"]).toBeUndefined();
    expect(route.request().postDataJSON()).toEqual({
      email,
      password: "a-secure-password",
    });
    return route.fulfill({
      json: bootstrap(true, { ...open, method: "password" }),
    });
  });
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await page.getByRole("link", { name: "Create an account" }).click();
  await expect(
    page.getByRole("heading", { name: "Create an account" }),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/signup");
  const emailField = page.getByLabel("Email", { exact: true });
  await expect(emailField).toHaveAttribute("type", "email");
  await emailField.fill("alice");
  await page.getByLabel("Password", { exact: true }).fill("a-secure-password");
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(emailField).toBeVisible();
  expect(
    await emailField.evaluate(
      (field) =>
        field instanceof HTMLInputElement && field.validity.typeMismatch,
    ),
  ).toBe(true);
  await emailField.fill(email);
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/");
  await page.getByRole("button", { name: "Account settings" }).click();
  await expect(
    page.getByText("No tokens yet.", { exact: false }),
  ).toBeVisible();
  await page
    .getByRole("navigation", { name: "Settings sections" })
    .getByRole("link", { name: "Password" })
    .click();
  await expect(page.getByRole("heading", { name: "Password" })).toBeVisible();
});

test("closed signup renders sign-in at the signup page", async ({ page }) => {
  await remote(page, false, { googleEnabled: true });
  for (const path of ["/signup", "/setup"]) {
    await page.goto(path);
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
    await expect(page.getByLabel("Password", { exact: true })).toHaveCount(0);
    await expect(
      page.getByRole("link", { name: "Create an account" }),
    ).toHaveCount(0);
  }
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
  await page.getByRole("button", { name: "New token" }).click();
  await expect(page.getByLabel("Token name")).toBeFocused();
  await page.getByLabel("Token name").fill("Laptop");
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  const heading = page.getByRole("heading", { name: "Save your token now" });
  await expect(page.getByLabel("Token name")).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Copy configuration" }),
  ).toBeFocused();
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
  const actions = page.getByRole("button", { name: "Actions for Laptop" });
  await actions.click();
  await page.getByRole("menuitem", { name: "Revoke…" }).click();
  const confirm = page.getByRole("dialog", { name: "Revoke Laptop?" });
  await expect(confirm.getByRole("button", { name: "Cancel" })).toBeFocused();
  await confirm.getByRole("button", { name: "Cancel" }).click();
  await expect(confirm).toBeHidden();
  await expect(page.getByText("Laptop", { exact: true })).toBeVisible();
  await actions.click();
  await page.getByRole("menuitem", { name: "Revoke…" }).click();
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
  await page.getByRole("button", { name: "Actions for Desktop" }).click();
  await page.getByRole("menuitem", { name: "Revoke…" }).click();
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
  await page.getByRole("button", { name: "New token" }).click();
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
  await page.goto("/settings/github");
  await expect(page.getByRole("heading", { name: "API tokens" })).toBeVisible();
  await expect(page).toHaveURL(/\/settings\/tokens$/);
  await expect(page.getByRole("heading", { name: "GitHub" })).toHaveCount(0);
  await expect(
    page
      .getByRole("navigation", { name: "Settings sections" })
      .getByRole("link", { name: "GitHub" }),
  ).toHaveCount(0);
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
  await page.goto("/settings/github?github=cancelled");
  await expect(
    page.getByText("GitHub authorization was cancelled."),
  ).toBeVisible();
  expect(new URL(page.url()).search).toBe("");
  const steps = page.getByRole("list", { name: "Set up GitHub" });
  await expect(steps.getByRole("heading")).toHaveText([
    "Connect your GitHub account",
    "Choose repositories for the diffx app",
  ]);
  await expect(steps.getByRole("button")).toHaveText(["Connect GitHub"]);
  await expect(
    steps.getByRole("link", { name: "Choose repositories on GitHub" }),
  ).toHaveAttribute("href", "https://github.com/apps/diffx/installations/new");
  await expect(page.getByText(/repositor(y|ies) available/)).toHaveCount(0);
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
  await page.goto("/settings/github");
  await expect(page.getByText("Connected as @alice-gh.")).toBeVisible();
  await expect(page.getByText("1 repository available.")).toBeVisible();
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

test("choosing repositories on GitHub reloads the list once on return", async ({
  page,
}) => {
  await remote(page);
  await page.route("**/api/v2/github/connection", (route) =>
    route.fulfill({
      json: {
        connected: true,
        login: "alice-gh",
        state: "active",
        installUrl: "https://github.test/apps/diffx/installations/new",
      },
    }),
  );
  let loads = 0;
  await page.route("**/api/v2/github/repositories", (route) => {
    loads += 1;
    return route.fulfill({
      json: {
        repositories:
          loads === 1
            ? []
            : [
                {
                  id: 10,
                  fullName: "octo/app",
                  private: false,
                  followed: false,
                  lost: false,
                },
              ],
      },
    });
  });
  await page
    .context()
    .route("https://github.test/**", (route) =>
      route.fulfill({ contentType: "text/html", body: "<h1>Install</h1>" }),
    );
  const returned = () =>
    page.evaluate(() => document.dispatchEvent(new Event("visibilitychange")));
  const noReload = () =>
    expect(
      page.waitForRequest("**/api/v2/github/repositories", { timeout: 500 }),
    ).rejects.toThrow();
  await page.goto("/settings/github");
  await expect(page.getByText("No repositories yet.")).toBeVisible();
  await expect(
    page.getByText(
      "No repositories to follow yet. Choose repositories on GitHub; this list updates when you return.",
    ),
  ).toBeVisible();
  await returned();
  await noReload();
  expect(loads).toBe(1);
  const install = page.waitForEvent("popup");
  await page
    .getByRole("link", { name: "Choose repositories on GitHub" })
    .click();
  await (await install).close();
  await returned();
  await expect(page.getByRole("checkbox", { name: "octo/app" })).toBeVisible();
  await expect(page.getByText("1 repository available.")).toBeVisible();
  expect(loads).toBe(2);
  await returned();
  await noReload();
  expect(loads).toBe(2);
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
  await page.goto("/settings/github");
  await expect(page.getByText(/expired\. Connect again/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Connect GitHub again" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Repositories", exact: true }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Actions for @alice-gh" }).click();
  await page.getByRole("menuitem", { name: "Disconnect…" }).click();
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
  await remote(page, false, { googleEnabled: true });
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
  await remote(page, false, { googleEnabled: true });
  await page.route("**/api/v2/invites/lookup", (route) => {
    expect(route.request().postDataJSON()).toEqual({ token: "invite-secret" });
    return route.fulfill({
      json: {
        email: "bob@example.com",
        expiresAt: Date.now() + 86_400_000,
      },
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

test("settings scroll at the window edge with a nav that marks the current page", async ({
  page,
}) => {
  await page.setViewportSize({ width: 1280, height: 600 });
  await remote(page, true, { method: "password" });
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
        repositories: Array.from({ length: 12 }, (_, index) => ({
          id: index,
          fullName: `octo/repo-${index}`,
          private: false,
          followed: false,
          lost: false,
        })),
      },
    }),
  );
  await page.goto("/settings");
  const scroller = page.getByRole("main");
  const box = await scroller.boundingBox();
  expect(box?.x).toBe(0);
  expect(box?.width).toBe(1280);
  await expect(
    page.getByRole("banner").getByText("alice@example.com", { exact: true }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/settings\/tokens$/);
  const nav = page.getByRole("navigation", { name: "Settings sections" });
  const current = nav.locator('[aria-current="page"]');
  await expect(nav.getByRole("link")).toHaveText([
    "API tokens",
    "GitHub",
    "Password",
  ]);
  await expect(current).toHaveText("API tokens");
  const navBox = await nav.boundingBox();
  await nav.getByRole("link", { name: "GitHub" }).click();
  await expect(page).toHaveURL(/\/settings\/github$/);
  await expect(current).toHaveText("GitHub");
  // Pages keep one width, so the nav does not move under the pointer.
  expect(await nav.boundingBox()).toEqual(navBox);
  await expect(
    page.getByRole("heading", { name: "GitHub", exact: true }),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "API tokens" })).toHaveCount(
    0,
  );
  await scroller.evaluate((element) => element.scrollTo({ top: 400 }));
  await expect(nav).toBeInViewport();
  await expect(current).toHaveText("GitHub");
  await nav.getByRole("link", { name: "Password" }).click();
  await expect(page).toHaveURL(/\/settings\/password$/);
  await expect(current).toHaveText("Password");
  await expect(page.getByRole("heading", { name: "Password" })).toBeVisible();
  expect(await scroller.evaluate((element) => element.scrollTop)).toBe(0);
  await page.goBack();
  await expect(page).toHaveURL(/\/settings\/github$/);
  await expect(current).toHaveText("GitHub");
  await page.setViewportSize({ width: 390, height: 800 });
  await expect(nav.getByRole("link", { name: "Password" })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Sign out", exact: true }),
  ).toBeVisible();
});

test("members see their Google email and no admin pages", async ({ page }) => {
  await remote(page, true, { email: "alice@example.com" });
  await page.goto("/settings");
  await expect(
    page.getByText("Signed in as alice@example.com with Google"),
  ).toBeVisible();
  await expect(page.getByRole("heading", { name: "Password" })).toHaveCount(0);
  await expect(page.getByText(/manage/i)).toHaveCount(0);
  for (const path of ["/manage", "/console", "/setup"]) {
    await page.goto(path);
    await expect(
      page.getByRole("heading", { name: "No review contexts yet" }),
    ).toBeVisible();
  }
});

test("an invite accepted with a password signs the account in", async ({
  page,
}) => {
  await remote(page, false, {
    googleEnabled: true,
    passwordSignupEnabled: true,
  });
  await page.route("**/api/v2/invites/lookup", (route) =>
    route.fulfill({
      json: {
        email: "bob@example.com",
        expiresAt: Date.now() + 86_400_000,
      },
    }),
  );
  await page.route("**/api/v2/invites/password", (route) => {
    expect(route.request().postDataJSON()).toEqual({
      token: "invite-secret",
      email: "bob@example.com",
      password: "a-secure-password",
    });
    return route.fulfill({
      json: bootstrap(true, {
        passwordSignupEnabled: true,
        method: "password",
      }),
    });
  });
  await page.goto("/invite#t=invite-secret");
  await expect(page.getByText("bob@example.com")).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Continue with Google" }),
  ).toBeVisible();
  await expect(page.getByLabel("Email", { exact: true })).toHaveValue(
    "bob@example.com",
  );
  await expect(page.getByLabel("Email", { exact: true })).toHaveAttribute(
    "readonly",
    "",
  );
  await page.getByLabel("Password", { exact: true }).fill("a-secure-password");
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toBeVisible();
});

test("a reset link sets a password and hides its secret", async ({ page }) => {
  await remote(page, false, { passwordSignupEnabled: true });
  await page.route("**/api/v2/password-reset/lookup", (route) =>
    route.fulfill({ json: { email: "alice@example.com" } }),
  );
  let completed: unknown;
  await page.route("**/api/v2/password-reset", (route) => {
    completed = route.request().postDataJSON();
    return route.fulfill({ status: 204 });
  });
  await page.goto("/reset-password#t=reset-secret");
  await expect(
    page.getByRole("heading", { name: "Reset password" }),
  ).toBeVisible();
  expect(new URL(page.url()).hash).toBe("");
  await page.getByLabel("New password", { exact: true }).fill("a-new-password");
  await page.getByRole("button", { name: "Set password" }).click();
  await expect(page.getByText("Your password is set")).toBeVisible();
  expect(completed).toEqual({
    token: "reset-secret",
    password: "a-new-password",
  });
  await page.getByRole("link", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});

test("another invite link opened in the same tab loads that invite", async ({
  page,
}) => {
  await remote(page, false, { googleEnabled: true });
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
