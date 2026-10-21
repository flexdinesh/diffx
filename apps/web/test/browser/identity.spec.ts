import { type Page } from "@playwright/test";
import { expect, test } from "./fixtures.ts";

const account = { id: "account-a", username: "alice" };
function bootstrap(signedIn: boolean, signupEnabled = false) {
  return {
    mode: "remote",
    signupEnabled,
    user: signedIn ? account : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}
async function remote(page: Page, signedIn = true, signupEnabled = false) {
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({ json: bootstrap(signedIn, signupEnabled) }),
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

test("disabled signup and invalid login preserve recovery", async ({
  page,
}) => {
  await remote(page, false);
  await page.route("**/api/v2/auth/login", (route) =>
    route.fulfill({
      status: 401,
      json: { detail: "Invalid username or password" },
    }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("button", { name: "Create an account" }),
  ).toHaveCount(0);
  await page.getByLabel("Username", { exact: true }).fill("alice");
  await page.getByLabel("Password", { exact: true }).fill("incorrect-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByRole("alert")).toHaveText(
    "Invalid username or password",
  );
  await expect(page.getByLabel("Username", { exact: true })).toHaveValue(
    "alice",
  );
});

test("signup opens an empty account whose settings work before any review", async ({
  page,
}) => {
  await remote(page, false, true);
  await page.route("**/api/v2/auth/signup", (route) =>
    route.fulfill({ json: bootstrap(true, true) }),
  );
  await page.goto("/");
  await page.getByRole("button", { name: "Create an account" }).click();
  await page.getByLabel("Username", { exact: true }).fill("alice");
  await page.getByLabel("Password", { exact: true }).fill("a-secure-password");
  await page
    .getByRole("button", { name: "Create account", exact: true })
    .click();
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Account settings" }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(
    page.getByRole("heading", { name: "Account settings" }),
  ).toBeVisible();
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
  await expect(page.locator(".token-secret pre")).toContainText(
    "one-time-secret",
  );
  const configuration: unknown = JSON.parse(
    await page.locator(".token-secret pre").innerText(),
  );
  expect(configuration).toEqual({
    remote: { url: new URL(page.url()).origin, token: "one-time-secret" },
  });
  await page.getByRole("button", { name: "Reviews", exact: true }).click();
  await page.getByRole("button", { name: "Account settings" }).click();
  await expect(page.locator(".token-secret")).toHaveCount(0);
  await expect(page.getByText("Laptop", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Revoke Laptop" }).click();
  await expect(page.getByText("Laptop", { exact: true })).toHaveCount(0);
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
  await remote(page);
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
  await expect(page.getByLabel("Password", { exact: true })).toHaveValue("");
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
