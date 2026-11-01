import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import type { ConsoleBootstrap } from "@diffx/console-api";
import { createElement } from "react";
import { Setup } from "../../src/auth.tsx";
import { renderPage, serve, signedIn } from "./session.ts";
import { json, takeNavigations } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  takeNavigations();
});

const setupRequired: ConsoleBootstrap = {
  setupRequired: true,
  googleEnabled: false,
  admin: null,
  csrfToken: null,
};

function openSetupLink() {
  window.history.replaceState(null, "", "/setup#t=setup-secret");
}

test("setup offers Google only when the console configures it", async () => {
  serve(setupRequired, (request) => {
    throw new Error(`Unexpected request: ${request.url}`);
  });
  openSetupLink();
  renderPage(createElement(Setup));
  await screen.findByRole("button", { name: "Create admin" });
  assert.equal(
    screen.queryByRole("button", { name: "Continue with Google" }),
    null,
  );
});

test("password setup requires email and submits the setup secret", async () => {
  const email = "admin.with.a.long.email.address@example.com";
  let body: unknown;
  serve(setupRequired, async (request) => {
    assert.equal(new URL(request.url).pathname, "/api/v1/setup");
    body = await request.json();
    return json(signedIn);
  });
  openSetupLink();
  renderPage(createElement(Setup));
  const field = await screen.findByLabelText("Email", { exact: true });
  assert.equal(field.getAttribute("type"), "email");
  assert.equal(field.getAttribute("maxlength"), "254");
  assert.equal(screen.queryByLabelText("Username", { exact: true }), null);
  fireEvent.change(field, { target: { value: email } });
  fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
    target: { value: "admin-password-1234" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create admin" }));
  await waitFor(() =>
    assert.deepEqual(body, {
      token: "setup-secret",
      email,
      password: "admin-password-1234",
    }),
  );
});

test("Google setup posts the link's secret and leaves for Google", async () => {
  const posted: { path: string; body: unknown }[] = [];
  serve({ ...setupRequired, googleEnabled: true }, async (request) => {
    posted.push({
      path: new URL(request.url).pathname,
      body: await request.json(),
    });
    return json({ authorizeUrl: "https://accounts.google.test/authorize" });
  });
  openSetupLink();
  renderPage(createElement(Setup));
  // The password form stays beside Google.
  await screen.findByRole("button", { name: "Create admin" });
  fireEvent.click(screen.getByRole("button", { name: "Continue with Google" }));
  await waitFor(() => assert.equal(takeNavigations(), 1));
  assert.deepEqual(posted, [
    { path: "/api/v1/setup/google", body: { token: "setup-secret" } },
  ]);
  // The secret never stays in the address bar.
  assert.equal(window.location.hash, "");
});

test("an invalid setup link reports it instead of leaving", async () => {
  serve({ ...setupRequired, googleEnabled: true }, () =>
    json({ title: "Not Found", status: 404, detail: "not found" }, 404),
  );
  openSetupLink();
  renderPage(createElement(Setup));
  fireEvent.click(
    await screen.findByRole("button", { name: "Continue with Google" }),
  );
  await screen.findByText(/This setup link is invalid or expired/);
  assert.equal(takeNavigations(), 0);
});
