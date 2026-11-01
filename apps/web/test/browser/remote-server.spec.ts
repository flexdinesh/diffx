import { execFile, execFileSync } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { type Page } from "@playwright/test";
import { reviewedToggle } from "./fixtures.ts";
import { expect, test } from "./remote-fixtures.ts";

const account = {
  email: "alice@example.com",
  password: "browser-password-0001",
};

/** A checkout with one uncommitted change for `diffx sync` to collect. */
async function changedCheckout(home: string) {
  const root = join(home, "browser-remote");
  const git = (...args: string[]) =>
    execFileSync(
      "git",
      [
        "-C",
        root,
        "-c",
        "user.name=Test",
        "-c",
        "user.email=test@example.com",
        ...args,
      ],
      { stdio: "ignore" },
    );
  execFileSync("git", ["init", "-q", "--initial-branch=main", root]);
  await writeFile(join(root, "value.txt"), "before\n");
  git("add", ".");
  git("commit", "-qm", "initial");
  await writeFile(join(root, "value.txt"), "after\n");
  return root;
}

async function createCollectorToken(page: Page) {
  await page.getByRole("button", { name: "Account settings" }).click();
  await page.getByLabel("Token name").fill("Browser sync");
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  const secret = page
    .getByRole("status")
    .filter({ has: page.getByRole("heading", { name: "Save your token now" }) })
    .locator("pre");
  await expect(secret).toContainText('"token"');
  const configuration: unknown = JSON.parse(await secret.innerText());
  if (
    typeof configuration !== "object" ||
    configuration === null ||
    !("remote" in configuration) ||
    typeof configuration.remote !== "object" ||
    configuration.remote === null ||
    !("token" in configuration.remote) ||
    typeof configuration.remote.token !== "string"
  )
    throw new Error("Missing collector token");
  return configuration.remote.token;
}

/** The review mark and comment come back from the server after a reload. */
async function expectStoredReview(page: Page) {
  await expect(
    page.getByRole("button", {
      name: /^value\.txt, .+, reviewed, 1 review comment$/,
    }),
  ).toBeVisible();
  await expect(page.locator("#review-count")).toHaveText("1 of 1 reviewed");
  await expect(
    reviewedToggle(page.locator("#viewer"), { pressed: true }),
  ).toHaveCount(1);
  await page.getByRole("tab", { name: "Review 1" }).click();
  await expect(
    page
      .locator('#comments-panel [data-comment-id] [data-slot="card"]')
      .filter({ hasText: "Check the new value." }),
  ).toBeVisible();
}

test("a signed-up user syncs a checkout and reviews it on a Postgres server", async ({
  page,
  browser,
  baseURL,
  fixtureBuild: { binary },
}) => {
  if (!binary || !baseURL) throw new Error("Missing diffx CLI build");
  const home = await mkdtemp(join(tmpdir(), "diffx-browser-remote-"));
  try {
    // Signup and password sign-in are on by default.
    await page.goto("/signup");
    await expect(
      page.getByRole("heading", { name: "Create an account" }),
    ).toBeVisible();
    await page.getByLabel("Email", { exact: true }).fill(account.email);
    await page.getByLabel("Password", { exact: true }).fill(account.password);
    await page
      .getByRole("button", { name: "Create account", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "No review contexts yet" }),
    ).toBeVisible();

    const token = await createCollectorToken(page);
    const root = await changedCheckout(home);
    await promisify(execFile)(binary, ["sync", "--path", root], {
      env: {
        ...process.env,
        DIFFX_CONFIG_PATH: join(home, "config.json"),
        DIFFX_RUNTIME_DIR: join(home, "runtime"),
        DIFFX_REMOTE_URL: baseURL,
        DIFFX_REMOTE_TOKEN: token,
      },
    });

    // The synced snapshot opens as the only review.
    await page.getByRole("button", { name: "Reviews", exact: true }).click();
    const file = page
      .locator("#viewer diffs-container")
      .filter({ hasText: "value.txt" });
    await expect(file.locator("[data-deletions] [data-line]")).toHaveText(
      /before/,
    );
    await expect(file.locator("[data-additions] [data-line]")).toHaveText(
      /after/,
    );
    await file
      .getByRole("button", {
        name: "Leave review comment on file value.txt",
        exact: true,
      })
      .click();
    const editor = file.locator('[data-slot="card"][data-active]');
    await editor
      .getByRole("textbox", { name: "Review comment" })
      .fill("Check the new value.");
    const saved = page.waitForResponse(
      (response) =>
        response.request().method() === "POST" &&
        /\/comments$/.test(new URL(response.url()).pathname),
    );
    await editor.getByRole("button", { name: "Save comment" }).click();
    expect((await saved).ok()).toBe(true);
    await expect(
      file
        .locator('[data-comment-id] [data-slot="card"]')
        .filter({ hasText: "Check the new value." }),
    ).toBeVisible();
    const marked = page.waitForResponse(
      (response) =>
        response.request().method() === "PUT" &&
        /\/review-marks\/[^/]+$/.test(new URL(response.url()).pathname),
    );
    await page
      .getByRole("button", { name: "Mark value.txt reviewed", exact: true })
      .click();
    expect((await marked).ok()).toBe(true);

    await page.reload();
    await expectStoredReview(page);

    // A fresh browser signs in with the password and sees the stored review.
    const context = await browser.newContext({ baseURL });
    try {
      const fresh = await context.newPage();
      await fresh.goto("/");
      await fresh.getByLabel("Email", { exact: true }).fill(account.email);
      await fresh
        .getByLabel("Password", { exact: true })
        .fill(account.password);
      await fresh.getByRole("button", { name: "Sign in", exact: true }).click();
      await expectStoredReview(fresh);
    } finally {
      await context.close();
    }
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});
