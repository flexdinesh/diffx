import assert from "node:assert/strict";
import { test } from "node:test";
import { readBootstrap, readAccountTokens, readCreatedToken } from "@diffx/api";

test("server mode requires explicit bootstrap and consistent session identity", () => {
  assert.deepEqual(
    readBootstrap({
      mode: "local",
      signupEnabled: false,
      user: null,
      csrfToken: null,
      collect: true,
    }),
    {
      mode: "local",
      signupEnabled: false,
      user: null,
      csrfToken: null,
      collect: true,
    },
  );
  assert.deepEqual(
    readBootstrap({
      mode: "remote",
      signupEnabled: false,
      user: null,
      csrfToken: null,
    }),
    { mode: "remote", signupEnabled: false, user: null, csrfToken: null },
  );
  for (const invalid of [
    null,
    {},
    {
      mode: "local",
      signupEnabled: true,
      user: null,
      csrfToken: null,
      collect: false,
    },
    { mode: "local", signupEnabled: false, user: null, csrfToken: null },
    {
      mode: "remote",
      signupEnabled: false,
      user: { id: "owner", username: "alice" },
      csrfToken: null,
    },
    { mode: "remote", signupEnabled: false, user: null, csrfToken: "secret" },
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
