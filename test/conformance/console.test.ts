import assert from "node:assert/strict";
import { type ChildProcess, execFile, spawn } from "node:child_process";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { createApiClient } from "../../packages/api/src/client.ts";
import { createConsoleClient } from "../../packages/console-api/src/client.ts";
import { serverDatabase } from "./postgres.ts";
import {
  conforms,
  consoleConforms,
  freePort,
  waitForServer,
} from "./support.ts";

const serverBinary = fileURLToPath(
  new URL("../../dist/diffx-server", import.meta.url),
);
const consoleBinary = fileURLToPath(
  new URL("../../dist/diffx-console", import.meta.url),
);
const run = promisify(execFile);
const password = "console-password-000001";

/** Resolves once the process exits; SIGTERM is the graceful stop. */
function stop(child: ChildProcess) {
  return new Promise<void>((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) {
      resolve();
      return;
    }
    child.once("exit", () => resolve());
    child.kill("SIGTERM");
  });
}

function cookieFrom(response: Response) {
  const cookie = response.headers.get("set-cookie")?.split(";")[0];
  assert.ok(cookie);
  return cookie;
}

test("The console administers the server's accounts over one database", async (t) => {
  const children: ChildProcess[] = [];
  t.after(() => Promise.all(children.map(stop)));
  const databaseUrl = await serverDatabase(t);
  const [serverPort, consolePort] = [await freePort(), await freePort()];
  const appUrl = `http://localhost:${serverPort}`;
  const consoleOrigin = `http://localhost:${consolePort}`;
  const consoleEnv = {
    ...process.env,
    DIFFX_CONSOLE_DATABASE_URL: databaseUrl,
    DIFFX_CONSOLE_PORT: String(consolePort),
    DIFFX_CONSOLE_PUBLIC_URL: consoleOrigin,
    DIFFX_CONSOLE_APP_URL: appUrl,
    DIFFX_CONSOLE_INSECURE_COOKIES: "true",
  };
  const server = spawn(serverBinary, ["--listen", `127.0.0.1:${serverPort}`], {
    stdio: "ignore",
    env: {
      ...process.env,
      DIFFX_SERVER_DATABASE_URL: databaseUrl,
      DIFFX_SERVER_PUBLIC_URL: appUrl,
      DIFFX_SERVER_INSECURE_COOKIES: "true",
    },
  });
  children.push(server);
  const consoleProcess = spawn(consoleBinary, [], {
    stdio: ["ignore", "ignore", "pipe"],
    env: consoleEnv,
  });
  children.push(consoleProcess);
  let consoleLog = "";
  consoleProcess.stderr?.setEncoding("utf8");
  consoleProcess.stderr?.on("data", (chunk: string) => (consoleLog += chunk));
  const serverUrl = `http://127.0.0.1:${serverPort}`;
  const consoleUrl = `http://127.0.0.1:${consolePort}`;
  await waitForServer(serverUrl);
  await waitForServer(consoleUrl, {}, "/readyz");
  const app = createApiClient({ baseUrl: serverUrl });
  const console = createConsoleClient({ baseUrl: consoleUrl });

  for (const probe of [
    await console.GET("/healthz"),
    await console.GET("/readyz"),
  ]) {
    assert.equal(probe.response.status, 200);
    assert.equal(probe.response.headers.get("cache-control"), "no-store");
    consoleConforms("Probe", probe.data);
  }
  const fresh = await console.GET("/api/v1/bootstrap");
  consoleConforms("Bootstrap", fresh.data);
  assert.deepEqual(fresh.data, {
    setupRequired: true,
    googleEnabled: false,
    admin: null,
    csrfToken: null,
  });

  // The first admin comes from the setup link the console logs.
  const token = /\/setup#t=(\S+)/.exec(consoleLog)?.[1];
  assert.ok(
    token && consoleLog.includes(`${consoleOrigin}/setup#t=`),
    consoleLog,
  );
  const invalidSetup = await console.POST("/api/v1/setup", {
    body: { token, email: "root", password },
  });
  assert.equal(invalidSetup.response.status, 400);
  const setup = await console.POST("/api/v1/setup", {
    body: { token, email: "root@example.com", password },
  });
  assert.equal(setup.response.status, 200);
  consoleConforms("Bootstrap", setup.data);
  assert.equal(setup.data?.admin?.email, "root@example.com");
  const csrf = setup.data?.csrfToken;
  assert.ok(csrf);
  const admin = { Cookie: cookieFrom(setup.response), "X-CSRF-Token": csrf };

  // The admin is a regular app account too.
  const adminApp = await app.POST("/api/v2/auth/login", {
    body: { email: "root@example.com", password },
  });
  assert.equal(adminApp.response.status, 200);
  conforms("Bootstrap", adminApp.data);

  // A member's app session never authenticates in the console.
  const signup = await app.POST("/api/v2/auth/signup", {
    body: { email: "member@example.com", password },
  });
  assert.equal(signup.response.status, 200);
  assert.ok(signup.data?.mode === "remote" && signup.data.csrfToken);
  const member = {
    Cookie: cookieFrom(signup.response),
    "X-CSRF-Token": signup.data.csrfToken,
  };
  const refused = await console.GET("/api/v1/accounts", {
    headers: { Cookie: member.Cookie },
  });
  assert.equal(refused.response.status, 401);
  const memberLogin = await console.POST("/api/v1/auth/login", {
    body: { email: "member@example.com", password },
  });
  assert.equal(memberLogin.response.status, 401);

  const accounts = await console.GET("/api/v1/accounts", { headers: admin });
  assert.equal(accounts.response.status, 200);
  for (const account of accounts.data?.accounts ?? [])
    consoleConforms("Account", account);
  const memberId = accounts.data?.accounts.find(
    (account) => account.email === "member@example.com",
  )?.id;
  assert.ok(memberId);

  // Invite and reset links open the app, which resolves them.
  const invite = await console.POST("/api/v1/invites", {
    headers: admin,
    body: { email: "new@example.com" },
  });
  assert.equal(invite.response.status, 201);
  consoleConforms("Invite", invite.data?.invite);
  const inviteToken = invite.data?.url.split(`${appUrl}/invite#t=`)[1];
  assert.ok(inviteToken);
  const lookup = await app.POST("/api/v2/invites/lookup", {
    body: { token: inviteToken },
  });
  assert.equal(lookup.response.status, 200);
  assert.equal(lookup.data?.email, "new@example.com");
  const mismatch = await app.POST("/api/v2/invites/password", {
    body: { token: inviteToken, email: "other@example.com", password },
  });
  assert.equal(mismatch.response.status, 400);
  const accepted = await app.POST("/api/v2/invites/password", {
    body: { token: inviteToken, email: "new@example.com", password },
  });
  assert.equal(accepted.response.status, 200);
  conforms("Bootstrap", accepted.data);
  assert.equal(
    accepted.data?.mode === "remote" && accepted.data.user?.email,
    "new@example.com",
  );
  const invites = await console.GET("/api/v1/invites", { headers: admin });
  for (const value of invites.data?.invites ?? [])
    consoleConforms("Invite", value);

  // Settings the console stores reach the server through the database.
  const stored = await console.GET("/api/v1/settings", { headers: admin });
  consoleConforms("Settings", stored.data);
  assert.deepEqual(stored.data, { signup: true, passwordSignup: true });
  const closed = await console.PUT("/api/v1/settings", {
    headers: admin,
    body: { signup: false, passwordSignup: true },
  });
  assert.equal(closed.response.status, 200);
  consoleConforms("Settings", closed.data);
  const deadline = Date.now() + 10_000;
  for (;;) {
    const bootstrap = await app.GET("/api/v2/bootstrap");
    if (bootstrap.data?.mode === "remote" && !bootstrap.data.signupEnabled)
      break;
    assert.ok(Date.now() < deadline, "server kept open signup");
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  const refusedSignup = await app.POST("/api/v2/auth/signup", {
    body: { email: "latecomer@example.com", password },
  });
  assert.equal(refusedSignup.response.status, 404);
  const reset = await console.POST(
    "/api/v1/accounts/{accountId}/password-reset",
    { headers: admin, params: { path: { accountId: memberId } } },
  );
  assert.equal(reset.response.status, 201);
  const resetToken = reset.data?.url.split(`${appUrl}/reset-password#t=`)[1];
  assert.ok(resetToken);
  const resetLookup = await app.POST("/api/v2/password-reset/lookup", {
    body: { token: resetToken },
  });
  assert.equal(resetLookup.data?.email, "member@example.com");

  // Offline recovery prints a link for the account's surface.
  const cli = await run(
    consoleBinary,
    ["user", "reset-password", "--email", "root@example.com"],
    { env: consoleEnv },
  );
  assert.match(cli.stdout, new RegExp(`${consoleOrigin}/reset-password#t=`));

  // Disabling in the console ends the member's stream on the server.
  const created = await app.POST("/api/v2/account/tokens", {
    headers: member,
    body: { name: "Console", purpose: "api", expiresInDays: 1 },
  });
  assert.equal(created.response.status, 201);
  const memberToken = created.data?.secret;
  assert.ok(memberToken);
  const events = await fetch(`${serverUrl}/api/v2/events`, {
    headers: { Authorization: `Bearer ${memberToken}` },
  });
  assert.equal(events.status, 200);
  const body = events.body;
  assert.ok(body);
  const drained = (async () => {
    for await (const _ of body);
  })();
  const disabled = await console.POST("/api/v1/accounts/{accountId}/disable", {
    headers: admin,
    params: { path: { accountId: memberId } },
  });
  assert.equal(disabled.response.status, 204);
  let timer: NodeJS.Timeout | undefined;
  await Promise.race([
    drained,
    new Promise((_, reject) => {
      timer = setTimeout(
        () => reject(new Error("server kept the stream open")),
        15_000,
      );
    }),
  ]);
  clearTimeout(timer);
  const health = await fetch(`${serverUrl}/api/v2/health`, {
    headers: { Authorization: `Bearer ${memberToken}` },
  });
  assert.equal(health.status, 401);
});
