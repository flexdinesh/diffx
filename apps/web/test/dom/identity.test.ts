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
  InviteSignUp,
  ResetPassword,
  SignIn,
  SignUp,
} from "../../src/account.tsx";
import { json, requests } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

type Policy = {
  method?: "google" | "password";
  google?: boolean;
  password?: boolean;
  signup?: boolean;
};

function bootstrap(
  signedIn = true,
  {
    method = "google",
    google = false,
    password = false,
    signup = false,
  }: Policy = {},
) {
  return {
    mode: "remote",
    googleEnabled: google,
    signupEnabled: signup,
    passwordSignupEnabled: password,
    user: signedIn
      ? { id: "account-a", username: "alice", email: null, method }
      : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}

function mount(Page: ComponentType, signedIn = true) {
  function Content() {
    const { state } = useIdentity();
    if (state.status !== "ready") return createElement(IdentityStatus);
    if (signedIn && !state.bootstrap.user) return createElement(SignIn);
    // Signed-out pages hand over to the app once they establish a session.
    if (!signedIn && state.bootstrap.user)
      return createElement("h1", null, "Signed in");
    return createElement(Page);
  }
  return render(createElement(IdentityProvider, null, createElement(Content)));
}

function remote(
  handler: (request: Request) => Response | Promise<Response>,
  signedIn = true,
  policy: Policy = {},
) {
  requests((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap") return json(bootstrap(signedIn, policy));
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

test("invite-only Google offers no password form or signup", async () => {
  remote(() => json({ detail: "Unexpected" }, 500), false, { google: true });
  mount(SignIn, false);
  await screen.findByRole("button", { name: "Continue with Google" });
  assert.equal(screen.queryByLabelText("Password", { exact: true }), null);
  assert.equal(screen.queryByRole("link", { name: "Create an account" }), null);
  assert.ok(screen.getByText("Accounts are by invite. Ask us for one."));
});

test("without password signup a quiet link reveals password sign-in", async () => {
  let body: unknown;
  remote(
    async (request) => {
      assert.equal(new URL(request.url).pathname, "/api/v2/auth/login");
      body = await request.json();
      return json(bootstrap(true, { method: "password", google: true }));
    },
    false,
    { google: true },
  );
  mount(SignIn, false);
  const google = await screen.findByRole("button", {
    name: "Continue with Google",
  });
  assert.equal(google.getAttribute("data-variant"), "default");
  assert.equal(
    screen.queryByLabelText("Email or username", { exact: true }),
    null,
  );
  fireEvent.click(
    screen.getByRole("button", { name: "Sign in with password" }),
  );
  const username = screen.getByLabelText("Email or username", { exact: true });
  assert.equal(document.activeElement, username);
  assert.equal(google.getAttribute("data-variant"), "outline");
  assert.equal(
    screen.queryByRole("button", { name: "Sign in with password" }),
    null,
  );
  fireEvent.change(username, { target: { value: "alice" } });
  fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
    target: { value: "old-password-123" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
  await screen.findByRole("heading", { name: "Signed in" });
  assert.deepEqual(body, { username: "alice", password: "old-password-123" });
});

test("with password signup the sign-in form shows without a reveal link", async () => {
  remote(() => json({ detail: "Unexpected" }, 500), false, {
    google: true,
    password: true,
  });
  mount(SignIn, false);
  await screen.findByRole("button", { name: "Continue with Google" });
  assert.ok(screen.getByLabelText("Password", { exact: true }));
  assert.equal(
    screen.queryByRole("button", { name: "Sign in with password" }),
    null,
  );
  assert.equal(screen.queryByRole("link", { name: "Create an account" }), null);
});

test("password signup is offered only while signup and password signup are on", async () => {
  remote(() => json({ detail: "Unexpected" }, 500), false, { signup: true });
  mount(SignIn, false);
  await screen.findByLabelText("Password", { exact: true });
  assert.equal(screen.queryByRole("link", { name: "Create an account" }), null);
  assert.ok(screen.getByText("Accounts are by invite. Ask us for one."));
  cleanup();

  remote(() => json({ detail: "Unexpected" }, 500), false, {
    signup: true,
    password: true,
  });
  mount(SignIn, false);
  assert.ok(await screen.findByRole("link", { name: "Create an account" }));
  cleanup();

  remote(() => json({ detail: "Unexpected" }, 500), false, {
    google: true,
    signup: true,
  });
  mount(SignUp, false);
  await screen.findByRole("button", { name: "Continue with Google" });
  assert.equal(screen.queryByLabelText("Password", { exact: true }), null);
});

test("failed password sign-in preserves recovery", async () => {
  remote(() => json({ detail: "Authentication required" }, 401), false, {
    google: true,
    password: true,
    signup: true,
  });
  mount(SignIn, false);
  assert.ok(
    await screen.findByRole("button", { name: "Continue with Google" }),
  );
  assert.ok(screen.getByRole("link", { name: "Create an account" }));
  fireEvent.change(
    screen.getByLabelText("Email or username", { exact: true }),
    {
      target: { value: "alice" },
    },
  );
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

test("password signup requires email and signs the new account in", async () => {
  const email = "alice.with.a.long.email.address@example.com";
  let body: unknown;
  remote(
    async (request) => {
      assert.equal(new URL(request.url).pathname, "/api/v2/auth/signup");
      body = await request.json();
      return json(bootstrap(true, { method: "password", password: true }));
    },
    false,
    { password: true, signup: true },
  );
  mount(SignUp, false);
  const field = await screen.findByLabelText("Email", { exact: true });
  assert.equal(field.getAttribute("type"), "email");
  assert.equal(field.getAttribute("maxlength"), "254");
  assert.equal(screen.queryByLabelText("Username", { exact: true }), null);
  fireEvent.change(field, {
    target: { value: email },
  });
  fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
    target: { value: "new-password-456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create account" }));
  await screen.findByRole("heading", { name: "Signed in" });
  assert.deepEqual(body, { email, password: "new-password-456" });
});

test("an invite accepts a password with its secret", async () => {
  window.history.replaceState(null, "", "/invite#t=invite-secret");
  const bodies: unknown[] = [];
  remote(
    async (request) => {
      const path = new URL(request.url).pathname;
      bodies.push(await request.json());
      if (path === "/api/v2/invites/lookup")
        return json({ email: "new@example.com", expiresAt: 0 });
      assert.equal(path, "/api/v2/invites/password");
      return json(bootstrap(true, { method: "password", password: true }));
    },
    false,
    { password: true },
  );
  mount(InviteSignUp, false);
  assert.ok(await screen.findByText("new@example.com"));
  assert.equal(window.location.hash, "");
  assert.equal(
    screen.queryByRole("button", { name: "Continue with Google" }),
    null,
  );
  assert.equal(screen.getByLabelText("Email").getAttribute("readonly"), "");
  assert.ok(screen.getByDisplayValue("new@example.com"));
  fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
    target: { value: "new-password-456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create account" }));
  await screen.findByRole("heading", { name: "Signed in" });
  assert.deepEqual(bodies, [
    { token: "invite-secret" },
    {
      token: "invite-secret",
      email: "new@example.com",
      password: "new-password-456",
    },
  ]);
});

test("a reset link sets a new password once and hides its secret", async () => {
  window.history.replaceState(null, "", "/reset-password#t=reset-secret");
  const bodies: unknown[] = [];
  remote(async (request) => {
    bodies.push(await request.json());
    return new URL(request.url).pathname === "/api/v2/password-reset/lookup"
      ? json({ username: "alice" })
      : new Response(null, { status: 204 });
  }, false);
  mount(ResetPassword, false);
  assert.ok(await screen.findByText("alice"));
  assert.equal(window.location.hash, "");
  fireEvent.change(screen.getByLabelText("New password", { exact: true }), {
    target: { value: "new-password-456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Set password" }));
  await screen.findByText(/Your password is set/);
  assert.deepEqual(bodies, [
    { token: "reset-secret" },
    { token: "reset-secret", password: "new-password-456" },
  ]);
});

test("an unusable reset link says so", async () => {
  window.history.replaceState(null, "", "/reset-password#t=used-secret");
  remote(() => json({ detail: "Reset link is invalid" }, 404), false);
  mount(ResetPassword, false);
  await screen.findByText(
    /This reset link is invalid, expired or already used/,
  );
  assert.equal(screen.queryByLabelText("New password", { exact: true }), null);
});

test("Google accounts have no password section", async () => {
  remote(() => json({ tokens: [] }), true, { password: true });
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
    { method: "password" },
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

test("without Google sign in shows the password form directly", async () => {
  window.history.replaceState(null, "", "/?signin=cancelled");
  remote(() => {
    throw new Error("Unexpected account request");
  }, false);
  mount(SignIn, false);
  await screen.findByText("Google sign-in was cancelled.");
  assert.ok(screen.getByLabelText("Password", { exact: true }));
  assert.equal(
    screen.queryByRole("button", { name: "Continue with Google" }),
    null,
  );
  assert.equal(
    screen.queryByRole("button", { name: "Sign in with password" }),
    null,
  );
  assert.equal(window.location.search, "");
});

test("an invite without password signup offers only Google", async () => {
  window.history.replaceState(null, "", "/invite#t=invite-secret");
  remote(() => json({ email: "new@example.com", expiresAt: 0 }), false, {
    google: true,
  });
  mount(InviteSignUp, false);
  const google = await screen.findByRole("button", {
    name: "Continue with Google",
  });
  assert.equal(google.getAttribute("data-variant"), "default");
  assert.equal(screen.queryByLabelText("Password", { exact: true }), null);
  assert.equal(screen.queryByText(/a password for this email/), null);
});

test("an unusable invite hides its secret and offers no Google action", async () => {
  window.history.replaceState(null, "", "/invite#t=used-secret");
  requests((request) =>
    new URL(request.url).pathname === "/api/v2/bootstrap"
      ? json(bootstrap(false, { google: true }))
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
