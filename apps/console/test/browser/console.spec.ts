import { type APIRequestContext, type Page } from "@playwright/test";
import { expect, root, signIn, test } from "./fixtures.ts";

// One deployment per worker: setup runs once, so the journeys run in order.
test.describe.configure({ mode: "serial" });

const memberPassword = "member-password-0001";

async function signUpMember(
  request: APIRequestContext,
  app: string,
  email: string,
) {
  const response = await request.post(`${app}/api/v2/auth/signup`, {
    data: { email, password: memberPassword },
  });
  expect(response.status()).toBe(200);
}

function accountRow(page: Page, email: string) {
  return page
    .getByRole("listitem")
    .filter({ has: page.getByText(email, { exact: true }) });
}

async function accountAction(page: Page, email: string, action: string) {
  await page.getByRole("button", { name: `Actions for ${email}` }).click();
  await page.getByRole("menuitem", { name: action }).click();
}

test("the setup link creates the first admin", async ({ page, deployment }) => {
  await page.goto("/");
  await expect(page.getByText("This server has no admin yet.")).toBeVisible();
  await page.goto(deployment.setup);
  // The secret leaves the address bar before any request.
  await expect(page).toHaveURL(/\/setup$/);
  await expect(
    page.getByRole("heading", { name: "Set up the console" }),
  ).toBeVisible();
  const email = page.getByLabel("Email", { exact: true });
  await email.fill("root");
  await page.getByLabel("Password").fill(root.password);
  await page.getByRole("button", { name: "Create admin" }).click();
  await expect(email).toBeVisible();
  expect(
    await email.evaluate(
      (field) =>
        field instanceof HTMLInputElement && field.validity.typeMismatch,
    ),
  ).toBe(true);
  await email.fill(root.email);
  await page.getByRole("button", { name: "Create admin" }).click();
  await expect(page).toHaveURL(/\/accounts$/);
  const self = accountRow(page, root.email);
  await expect(self.getByText("You")).toBeVisible();
  await expect(self.getByText("Admin")).toBeVisible();
  // Admins take no actions on themselves.
  await expect(
    page.getByRole("button", { name: `Actions for ${root.email}` }),
  ).toHaveCount(0);

  // The link works once.
  await page.getByRole("link", { name: "Admin" }).click();
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await page.goto(deployment.setup);
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});

test("only admins sign in", async ({ page, request, deployment }) => {
  await signUpMember(request, deployment.app, "member@example.com");
  await signIn(page, {
    email: "member@example.com",
    password: memberPassword,
  });
  await expect(page.getByRole("alert")).toContainText("Unable to sign in");
  await signIn(page, root);
  await expect(page.getByRole("heading", { name: "Accounts" })).toBeVisible();
  // Unknown paths land on Accounts.
  await page.goto("/manage");
  await expect(page).toHaveURL(/\/accounts$/);
});

test("admins manage roles, access and recovery", async ({
  page,
  request,
  deployment,
}) => {
  await signUpMember(request, deployment.app, "alice@example.com");
  await signIn(page, root);
  const alice = accountRow(page, "alice@example.com");
  await expect(alice).toContainText("Password");

  await accountAction(page, "alice@example.com", "Make admin");
  await expect(
    page.getByText("alice@example.com is now an admin."),
  ).toBeVisible();
  await expect(alice.getByText("Admin")).toBeVisible();

  // An admin's reset link opens this console.
  await accountAction(page, "alice@example.com", "Create reset link");
  const panel = page
    .getByRole("status")
    .filter({ hasText: "Send this reset link to alice@example.com" });
  await expect(panel).toContainText("It opens this console");
  const adminLink = await panel.locator("pre").innerText();
  expect(adminLink.startsWith(`${deployment.origin}/reset-password#t=`)).toBe(
    true,
  );

  // A member's reset link opens the app.
  await accountAction(page, "member@example.com", "Create reset link");
  await expect(
    page
      .getByRole("status")
      .filter({ hasText: "Send this reset link to member@example.com" }),
  ).toContainText(`${deployment.app}/reset-password#t=`);

  await accountAction(page, "member@example.com", "Disable…");
  const disable = page.getByRole("dialog");
  await expect(disable).toContainText("Disable member@example.com?");
  await disable.getByRole("button", { name: "Disable account" }).click();
  await expect(
    accountRow(page, "member@example.com").getByText("Disabled"),
  ).toBeVisible();
  await accountAction(page, "member@example.com", "Enable");
  await expect(
    accountRow(page, "member@example.com").getByText("Disabled"),
  ).toHaveCount(0);

  await accountAction(page, "member@example.com", "Delete…");
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Delete account" })
    .click();
  await expect(accountRow(page, "member@example.com")).toHaveCount(0);

  // alice sets her password from the console link, then signs in here.
  const other = await page.context().browser()?.newPage();
  if (!other) throw new Error("Missing browser");
  await other.goto(adminLink);
  await expect(other).toHaveURL(/\/reset-password$/);
  await expect(other.getByText("Choose a new password for")).toContainText(
    "alice@example.com",
  );
  await other.getByLabel("New password").fill("alice-console-password-1");
  await other.getByRole("button", { name: "Set password" }).click();
  await expect(other.getByText("Your password is set")).toBeVisible();
  await other.getByRole("link", { name: "Sign in" }).click();
  await other.getByLabel("Email").fill("alice@example.com");
  await other.getByLabel("Password").fill("alice-console-password-1");
  await other.getByRole("button", { name: "Sign in" }).click();
  await expect(other.getByRole("heading", { name: "Accounts" })).toBeVisible();
  await other.close();
});

test("admins invite members to the app", async ({
  page,
  request,
  deployment,
}) => {
  await signIn(page, root);
  await page.getByRole("link", { name: "Invites" }).click();
  await expect(page.getByText("No invites yet.")).toBeVisible();
  await page.getByLabel("Email").fill("dana@example.com");
  // Admins come from promoting accounts, never from invites.
  await expect(page.getByRole("checkbox")).toHaveCount(0);
  await page.getByRole("button", { name: "Create invite" }).click();
  const panel = page
    .getByRole("status")
    .filter({ hasText: "Send this invite link" });
  await expect(panel).toContainText("for dana@example.com, until");
  const link = await panel.locator("pre").innerText();
  const token = link.split(`${deployment.app}/invite#t=`)[1];
  expect(token).toBeTruthy();
  // The app resolves the console's invite as a member invite.
  const lookup = await request.post(`${deployment.app}/api/v2/invites/lookup`, {
    data: { token },
  });
  const invited: unknown = await lookup.json();
  expect(invited).toMatchObject({ email: "dana@example.com" });
  expect(invited).not.toHaveProperty("admin");

  const invite = page.getByRole("listitem").filter({ hasText: "dana@" });
  await expect(invite.getByText("Pending")).toBeVisible();
  await expect(invite.getByText("Admin")).toHaveCount(0);
  await page
    .getByRole("button", { name: "Revoke invite for dana@example.com" })
    .click();
  await page
    .getByRole("dialog")
    .getByRole("button", { name: "Revoke invite" })
    .click();
  await expect(invite.getByText("Revoked")).toBeVisible();
  await expect(panel).toHaveCount(0);
});

test("admins set who can join the app", async ({
  page,
  request,
  deployment,
}) => {
  async function appBootstrap(): Promise<unknown> {
    const response = await request.get(`${deployment.app}/api/v2/bootstrap`);
    return response.json();
  }
  await signIn(page, root);
  await page.getByRole("link", { name: "Settings" }).click();
  await expect(page.getByRole("link", { name: "Settings" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  const signup = page.getByRole("checkbox", { name: "Open signup" });
  const password = page.getByRole("checkbox", { name: "Password signup" });
  const status = page.getByText("Settings saved.");
  const opened = await signup.isChecked();
  const passwords = await password.isChecked();

  await signup.click();
  await expect(status).toBeVisible();
  await expect(signup).toBeChecked({ checked: !opened });
  // Server replicas apply saved settings within moments.
  await expect.poll(appBootstrap).toMatchObject({ signupEnabled: !opened });

  await password.click();
  await expect(password).toBeChecked({ checked: !passwords });
  await expect.poll(appBootstrap).toMatchObject({
    signupEnabled: !opened,
    passwordSignupEnabled: !passwords,
  });

  // Saved settings survive a reload.
  await page.reload();
  await expect(signup).toBeChecked({ checked: !opened });
  await expect(password).toBeChecked({ checked: !passwords });
  await signup.click();
  await expect(signup).toBeChecked({ checked: opened });
  await password.click();
  await expect(password).toBeChecked({ checked: passwords });
});

test("a password change signs the admin out", async ({ page }) => {
  await signIn(page, root);
  await page.getByRole("link", { name: "Admin" }).click();
  await expect(page.getByRole("link", { name: "Admin" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await page.getByLabel("Current password").fill("wrong-password-0000");
  await page.getByLabel("New password").fill("root-console-password-2");
  await page
    .getByRole("button", { name: "Change password and sign out" })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Current password incorrect",
  );
  await page.getByLabel("Current password").fill(root.password);
  await page
    .getByRole("button", { name: "Change password and sign out" })
    .click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await signIn(page, {
    email: root.email,
    password: "root-console-password-2",
  });
  await expect(page.getByRole("heading", { name: "Accounts" })).toBeVisible();
});
