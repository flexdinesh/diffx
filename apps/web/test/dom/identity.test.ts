import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement, type ComponentType } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import {
  IdentityProvider,
  IdentityStatus,
  useIdentity,
} from "../../src/identity-context.tsx";
import {
  AccountSettings,
  AdminSetup,
  AdminSignIn,
  InviteSignUp,
  SignIn,
} from "../../src/account.tsx";
import { json, requests } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

function bootstrap(signedIn = true, admin = false) {
  return {
    mode: "remote",
    googleEnabled: false,
    user: signedIn
      ? { id: "account-a", username: "alice", email: null, admin }
      : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}

function mount(Page: ComponentType, signedIn = true) {
  function Content() {
    const { state } = useIdentity();
    if (state.status !== "ready") return createElement(IdentityStatus);
    if (signedIn && !state.bootstrap.user) return createElement(SignIn);
    return createElement(Page);
  }
  return render(createElement(IdentityProvider, null, createElement(Content)));
}

function remote(
  handler: (request: Request) => Response | Promise<Response>,
  signedIn = true,
  admin = false,
) {
  requests((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap") return json(bootstrap(signedIn, admin));
    if (path === "/api/v2/github/connection") return json({}, 404);
    if (path === "/api/v2/push") return json({}, 404);
    return handler(request);
  });
}

test("bootstrap failure stays an error and retries without inferring local mode", async () => {
  let failed = true;
  requests(() =>
    failed
      ? json({ detail: "Identity unavailable" }, 503)
      : json(bootstrap(false)),
  );
  mount(SignIn, false);
  await screen.findByRole("heading", { name: "Cannot load diffx" });
  assert.ok(screen.getByText("Identity unavailable"));
  failed = false;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByRole("heading", { name: "Sign in" });
});

test("members get no password form; invalid admin login preserves recovery", async () => {
  remote(() => json({ detail: "Authentication required" }, 401), false);
  const view = mount(SignIn, false);
  await screen.findByRole("heading", { name: "Sign in" });
  assert.equal(screen.queryByLabelText("Password", { exact: true }), null);
  view.unmount();
  mount(AdminSignIn, false);
  fireEvent.change(await screen.findByLabelText("Username", { exact: true }), {
    target: { value: "alice" },
  });
  fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
    target: { value: "incorrect-password" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  assert.equal(
    (await screen.findByRole("alert")).textContent,
    "Authentication required",
  );
  assert.equal(
    screen.getByDisplayValue("alice").getAttribute("id"),
    "username",
  );
});

test("a completed setup is the sign-in page and drops the stale secret", async () => {
  window.history.replaceState(null, "", "/setup#t=stale-secret");
  remote(() => json({ required: false }), false);
  mount(AdminSetup, false);
  await screen.findByRole("heading", { name: "Sign in" });
  assert.equal(screen.queryByRole("heading", { name: "Set up diffx" }), null);
  assert.equal(document.querySelector('a[href="/console"]'), null);
  assert.equal(screen.queryByText(/admin/i), null);
  assert.equal(window.location.hash, "");
  assert.equal(screen.queryByLabelText("Password", { exact: true }), null);
});

test("member settings have no password section", async () => {
  remote(() => json({ tokens: [] }));
  mount(AccountSettings);
  await screen.findByRole("heading", { name: "API tokens" });
  assert.equal(screen.queryByRole("heading", { name: "Password" }), null);
});

test("token list failure hides its form and retries through the shared action", async () => {
  let failed = true;
  remote(() =>
    failed ? json({ detail: "Tokens unavailable" }, 503) : json({ tokens: [] }),
  );
  mount(AccountSettings);
  assert.match(
    (await screen.findByRole("alert")).textContent ?? "",
    /Tokens unavailable/,
  );
  assert.equal(screen.queryByLabelText("Token name"), null);
  failed = false;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByLabelText("Token name");
  assert.equal(screen.queryByRole("alert"), null);
});

test("expired account requests dispose account UI and clear the route", async () => {
  window.history.replaceState(null, "", "/settings");
  remote(() => json({ detail: "Session expired" }, 401));
  mount(AccountSettings);
  await screen.findByRole("heading", { name: "Sign in" });
  assert.equal(
    screen.queryByRole("heading", { name: "Account settings" }),
    null,
  );
  assert.equal(window.location.pathname, "/");
});

test("password change sends CSRF and signs out after revoking sessions", async () => {
  let body: unknown;
  remote(
    async (request) => {
      if (request.method === "GET") return json({ tokens: [] });
      assert.equal(request.headers.get("X-CSRF-Token"), "session-csrf");
      body = await request.json();
      return new Response(null, { status: 204 });
    },
    true,
    true,
  );
  mount(AccountSettings);
  fireEvent.change(
    await screen.findByLabelText("Current password", { exact: true }),
    { target: { value: "old-password-123" } },
  );
  fireEvent.change(screen.getByLabelText("New password", { exact: true }), {
    target: { value: "new-password-456" },
  });
  fireEvent.click(
    screen.getByRole("button", { name: "Change password and sign out" }),
  );
  await screen.findByRole("heading", { name: "Sign in" });
  assert.deepEqual(body, {
    currentPassword: "old-password-123",
    newPassword: "new-password-456",
  });
});

test("logout failure stays recoverable and success disposes account UI", async () => {
  let failed = true;
  remote((request) => {
    if (request.method === "GET") return json({ tokens: [] });
    assert.equal(request.headers.get("X-CSRF-Token"), "session-csrf");
    return failed
      ? json({ detail: "Logout unavailable" }, 503)
      : new Response(null, { status: 204 });
  });
  mount(AccountSettings);
  fireEvent.click(await screen.findByRole("button", { name: "Sign out" }));
  assert.equal(
    (await screen.findByRole("alert")).textContent,
    "Logout unavailable",
  );
  failed = false;
  fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
  await screen.findByRole("heading", { name: "Sign in" });
});

test("without Google configuration sign in offers no Google action", async () => {
  window.history.replaceState(null, "", "/?signin=cancelled");
  remote(() => {
    throw new Error("Unexpected account request");
  }, false);
  mount(SignIn, false);
  await screen.findByText(/Sign-in is not available on this server yet/);
  assert.equal(
    screen.queryByRole("button", { name: "Continue with Google" }),
    null,
  );
  assert.equal(window.location.search, "");
});

test("an unusable invite hides its secret and offers no Google action", async () => {
  window.history.replaceState(null, "", "/invite#t=used-secret");
  requests((request) =>
    new URL(request.url).pathname === "/api/v2/bootstrap"
      ? json({ ...bootstrap(false), googleEnabled: true })
      : json(
          { detail: "Invite link is invalid, expired or already used" },
          404,
        ),
  );
  mount(InviteSignUp, false);
  await screen.findByText(
    /This invite link is invalid, expired or already used/,
  );
  await waitFor(() => assert.equal(window.location.hash, ""));
  assert.equal(
    screen.queryByRole("button", { name: "Continue with Google" }),
    null,
  );
});
