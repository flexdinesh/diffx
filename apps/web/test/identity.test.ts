import assert from "node:assert/strict";
import { test } from "node:test";
import { readBootstrap, readAccountTokens, readCreatedToken } from "@diffx/api";

test("server mode requires explicit bootstrap and consistent session identity", () => {
  for (const input of ["checkout", "checkouts", "capture"]) {
    const bootstrap = {
      mode: "local",
      googleEnabled: false,
      user: null,
      csrfToken: null,
      collect: input !== "capture",
      input,
    };
    assert.deepEqual(readBootstrap(bootstrap), bootstrap);
  }
  assert.throws(() =>
    readBootstrap({
      mode: "local",
      googleEnabled: false,
      user: null,
      csrfToken: null,
      collect: true,
      input: "worktrees",
    }),
  );
  assert.deepEqual(
    readBootstrap({
      mode: "local",
      googleEnabled: false,
      user: null,
      csrfToken: null,
      collect: true,
      input: "checkouts",
    }),
    {
      mode: "local",
      googleEnabled: false,
      user: null,
      csrfToken: null,
      collect: true,
      input: "checkouts",
    },
  );
  assert.deepEqual(
    readBootstrap({
      mode: "remote",
      publicPullRequests: false,
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: null,
      csrfToken: null,
    }),
    {
      mode: "remote",
      publicPullRequests: false,
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: null,
      csrfToken: null,
    },
  );
  assert.deepEqual(
    readBootstrap({
      mode: "remote",
      publicPullRequests: true,
      googleEnabled: true,
      signupEnabled: false,
      passwordSignupEnabled: false,
      user: { id: "owner", email: "alice@example.com", method: "google" },
      csrfToken: "secret",
    }),
    {
      mode: "remote",
      publicPullRequests: true,
      googleEnabled: true,
      signupEnabled: false,
      passwordSignupEnabled: false,
      user: { id: "owner", email: "alice@example.com", method: "google" },
      csrfToken: "secret",
    },
  );
  for (const invalid of [
    null,
    {},
    {
      mode: "local",
      googleEnabled: true,
      user: null,
      csrfToken: null,
      collect: false,
      input: "capture",
    },
    { mode: "local", googleEnabled: false, user: null, csrfToken: null },
    {
      mode: "local",
      googleEnabled: false,
      user: null,
      csrfToken: null,
      collect: false,
    },
    {
      mode: "local",
      googleEnabled: false,
      user: null,
      csrfToken: null,
      collect: false,
      input: "stdin",
    },
    // Remote bootstraps must report both sign-in settings.
    { mode: "remote", googleEnabled: false, user: null, csrfToken: null },
    // ...and whether public pull request previews are on.
    {
      mode: "remote",
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: null,
      csrfToken: null,
    },
    {
      mode: "remote",
      publicPullRequests: "yes",
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: null,
      csrfToken: null,
    },
    {
      mode: "remote",
      publicPullRequests: false,
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: { id: "owner", email: "alice@example.com", method: "password" },
      csrfToken: null,
    },
    {
      mode: "remote",
      publicPullRequests: false,
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: { id: "owner", email: "alice@example.com" },
      csrfToken: "secret",
    },
    // Every account has an email.
    {
      mode: "remote",
      publicPullRequests: false,
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: { id: "owner", email: null, method: "password" },
      csrfToken: "secret",
    },
    {
      mode: "remote",
      publicPullRequests: true,
      googleEnabled: true,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: { id: "owner", email: "alice@example.com", method: "sso" },
      csrfToken: "secret",
    },
    {
      mode: "remote",
      publicPullRequests: false,
      googleEnabled: false,
      signupEnabled: true,
      passwordSignupEnabled: true,
      user: null,
      csrfToken: "secret",
    },
  ])
    assert.throws(
      () => readBootstrap(invalid),
      /Invalid server identity response/,
    );
});

test("account credentials reject malformed metadata and absent one-time secrets", () => {
  const token = {
    id: "token",
    name: "Laptop",
    purpose: "collector",
    createdAt: 1000,
    lastUsedAt: null,
    expiresAt: 2000,
  };
  assert.deepEqual(readAccountTokens({ tokens: [token] }), [token]);
  assert.deepEqual(readCreatedToken({ token, secret: "secret" }), {
    token,
    secret: "secret",
  });
  assert.throws(
    () => readAccountTokens({ tokens: [{ ...token, purpose: "admin" }] }),
    /Invalid token response/,
  );
  assert.throws(
    () => readAccountTokens({ tokens: [{ ...token, expiresAt: 1.5 }] }),
    /Invalid token response/,
  );
  assert.throws(() => readCreatedToken({ token }), /Invalid token response/);
});
