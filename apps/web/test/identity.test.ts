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
      googleEnabled: false,
      user: null,
      csrfToken: null,
    }),
    { mode: "remote", googleEnabled: false, user: null, csrfToken: null },
  );
  assert.deepEqual(
    readBootstrap({
      mode: "remote",
      googleEnabled: true,
      user: { id: "owner", username: "alice", email: null, admin: true },
      csrfToken: "secret",
    }),
    {
      mode: "remote",
      googleEnabled: true,
      user: { id: "owner", username: "alice", email: null, admin: true },
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
    {
      mode: "remote",
      googleEnabled: false,
      user: { id: "owner", username: "alice", email: null, admin: false },
      csrfToken: null,
    },
    {
      mode: "remote",
      googleEnabled: false,
      user: { id: "owner", username: "alice" },
      csrfToken: "secret",
    },
    {
      mode: "remote",
      googleEnabled: true,
      user: { id: "owner", username: "alice", admin: false },
      csrfToken: "secret",
    },
    { mode: "remote", googleEnabled: false, user: null, csrfToken: "secret" },
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
