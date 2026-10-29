import assert from "node:assert/strict";
import {
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Ajv2020 } from "ajv/dist/2020.js";
import { parse } from "yaml";

const contract: unknown = parse(
  readFileSync(
    new URL("../../packages/api/openapi.yaml", import.meta.url),
    "utf8",
  ),
);
assert.ok(typeof contract === "object" && contract !== null);
const validator = new Ajv2020({ strict: false, validateFormats: false });
validator.addSchema(contract, "api");
function conforms(name: string, value: unknown) {
  const check = validator.compile({ $ref: "api#/components/schemas/" + name });
  assert.ok(check(value), JSON.stringify(check.errors));
}
import { execFileSync, spawn } from "node:child_process";
import { createECDH, randomBytes } from "node:crypto";
import { createServer } from "node:net";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { createApiClient } from "../../packages/api/src/client.ts";
import { serverDatabase } from "./postgres.ts";
import type { components } from "../../packages/api/src/schema.d.ts";

const fixture = fileURLToPath(
  new URL("../fixtures/sample.diff", import.meta.url),
);
const binary = fileURLToPath(new URL("../../dist/diffx", import.meta.url));
const remoteBinary = fileURLToPath(
  new URL("../../dist/diffx-server", import.meta.url),
);
const apiValues = { allScope: "all" } satisfies { allScope: "all" };

async function freePort() {
  const server = createServer();
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    server.close();
    throw new Error("Unable to reserve port");
  }
  await new Promise<void>((resolve, reject) =>
    server.close((error) => (error ? reject(error) : resolve())),
  );
  return address.port;
}

async function waitForServer(
  url: string,
  headers: Record<string, string> = {},
) {
  let lastError: unknown;
  for (let attempt = 0; attempt < 200; attempt++) {
    try {
      const response = await fetch(`${url}/api/v2/bootstrap`, { headers });
      if (response.ok) return;
    } catch (error) {
      lastError = error;
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error("Go server did not start", { cause: lastError });
}

test("Local workspace collection implements the API contract", async (t) => {
  const home = mkdtempSync(join(tmpdir(), "diffx-conformance-"));
  const workspace = join(home, "workspace");
  for (const name of ["first", "second"]) {
    const root = join(workspace, name);
    mkdirSync(root, { recursive: true });
    const git = (...args: string[]) =>
      execFileSync("git", ["-C", root, ...args], { stdio: "ignore" });
    git("init", "-q");
    writeFileSync(join(root, "file.txt"), "before\n");
    git("add", ".");
    git(
      "-c",
      "user.name=Test",
      "-c",
      "user.email=test@example.com",
      "commit",
      "-qm",
      "initial",
    );
    writeFileSync(join(root, "file.txt"), `${name}\n`);
  }
  const port = await freePort();
  const server = spawn(
    binary,
    [
      "--path",
      workspace,
      "--state",
      "memory",
      "--no-browser",
      "--port",
      String(port),
    ],
    {
      stdio: "ignore",
      env: {
        ...process.env,
        DIFFX_RUNTIME_DIR: join(home, "runtime"),
        DIFFX_CONFIG_PATH: join(home, "config.json"),
        DIFFX_SOURCE_ID: "conformance",
      },
    },
  );
  t.after(() => {
    server.kill("SIGTERM");
    rmSync(home, { recursive: true, force: true });
  });
  const url = `http://127.0.0.1:${port}`;
  await waitForServer(url);
  const client = createApiClient({ baseUrl: url });
  const bootstrap = await client.GET("/api/v2/bootstrap");
  conforms("Bootstrap", bootstrap.data);
  assert.ok(bootstrap.data?.mode === "local" && bootstrap.data.collect);
  const collection = await client.POST("/api/v2/collections");
  conforms("CollectionResult", collection.data);
  assert.equal(collection.data?.contexts.length, 2);
  assert.deepEqual(collection.data?.failures, []);
});

test("Captured branch commits implement the API contract", async (t) => {
  const home = mkdtempSync(join(tmpdir(), "diffx-commits-"));
  const root = join(home, "repository");
  mkdirSync(root, { recursive: true });
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
  git("init", "-q", "--initial-branch=main");
  writeFileSync(join(root, "file.txt"), "before\n");
  git("add", ".");
  git("commit", "-qm", "initial");
  git("switch", "-q", "--create", "feature");
  writeFileSync(join(root, "file.txt"), "first\n");
  git("commit", "-qam", "First change");
  writeFileSync(join(root, "file.txt"), "second\n");
  git("commit", "-qam", "Second change");
  const port = await freePort();
  const server = spawn(
    binary,
    [root, "--state", "memory", "--no-browser", "--port", String(port)],
    {
      stdio: "ignore",
      env: {
        ...process.env,
        DIFFX_RUNTIME_DIR: join(home, "runtime"),
        DIFFX_CONFIG_PATH: join(home, "config.json"),
        DIFFX_SOURCE_ID: "conformance",
      },
    },
  );
  t.after(() => {
    server.kill("SIGTERM");
    rmSync(home, { recursive: true, force: true });
  });
  const url = `http://127.0.0.1:${port}`;
  await waitForServer(url);
  const client = createApiClient({ baseUrl: url });
  const collection = await client.POST("/api/v2/collections");
  const context = collection.data?.contexts[0];
  assert.ok(context);
  conforms("Context", context);
  assert.deepEqual(context.commits, { count: 2, omitted: 0 });
  const path = { contextId: context.id };
  const listed = await client.GET("/api/v2/contexts/{contextId}/commits", {
    params: { path },
  });
  conforms("CommitList", listed.data);
  const commits = listed.data?.commits ?? [];
  assert.deepEqual(
    commits.map((commit) => commit.subject),
    ["First change", "Second change"],
  );
  const second = commits[1];
  assert.ok(second && !second.merge && second.fileCount === 1);
  const diff = await client.GET(
    "/api/v2/contexts/{contextId}/commits/{oid}/diff",
    { params: { path: { ...path, oid: second.oid } } },
  );
  conforms("RepositoryDiff", diff.data);
  assert.equal(diff.data?.mode, "commit");
  assert.equal(diff.data?.comparisonKind, "commit");
  const file = diff.data?.files[0];
  assert.ok(file);
  const patch = await client.GET(
    "/api/v2/contexts/{contextId}/commits/{oid}/files/{fileId}/patch",
    {
      params: {
        path: { ...path, oid: second.oid, fileId: file.id },
        query: { fileVersion: file.fingerprint },
      },
    },
  );
  conforms("FilePatch", patch.data);
  assert.match(patch.data?.patch ?? "", /\+second/);
  assert.doesNotMatch(patch.data?.patch ?? "", /\+first/);
  const contents = await client.GET(
    "/api/v2/contexts/{contextId}/commits/{oid}/files/{fileId}/contents",
    {
      params: {
        path: { ...path, oid: second.oid, fileId: file.id },
        query: { fileVersion: file.fingerprint },
      },
    },
  );
  conforms("FileContents", contents.data);
  assert.deepEqual(contents.data, { before: "first\n", after: "second\n" });
  const missing = await client.GET(
    "/api/v2/contexts/{contextId}/commits/{oid}/diff",
    { params: { path: { ...path, oid: "0".repeat(40) } } },
  );
  assert.equal(missing.response.status, 404);
  conforms("Problem", missing.error);
});

test("Go distribution implements the API contract", async (t) => {
  const port = await freePort();
  const server = spawn(
    binary,
    [
      "dev",
      "--fixture",
      fixture,
      "--state",
      "memory",
      "--no-browser",
      "--port",
      String(port),
    ],
    { stdio: "ignore" },
  );
  t.after(() => server.kill("SIGTERM"));
  const url = `http://127.0.0.1:${port}`;
  await waitForServer(url);
  const client = createApiClient({ baseUrl: url });
  const localBootstrap = await client.GET("/api/v2/bootstrap");
  conforms("Bootstrap", localBootstrap.data);
  assert.deepEqual(localBootstrap.data, {
    mode: "local",
    googleEnabled: false,
    user: null,
    csrfToken: null,
    collect: false,
    input: "capture",
  });
  const collection = await fetch(`${url}/api/v2/collections`, {
    method: "POST",
  });
  assert.equal(collection.status, 404);
  const health = await client.GET("/api/v2/health");
  assert.equal(health.data?.ingestionEnabled, false);
  assert.equal(health.data?.queuedIngestion, false);

  const catalog = await client.GET("/api/v2/contexts");
  assert.equal(catalog.data?.contexts.length, 1);
  const context = catalog.data?.contexts[0];
  assert.ok(context);
  conforms("ContextPage", catalog.data);
  conforms("Context", context);
  assert.equal(context.kind, "observation");
  const contextId = context.id;

  const comments = await client.GET("/api/v2/contexts/{contextId}/comments", {
    params: { path: { contextId } },
  });
  assert.deepEqual(comments.data, { comments: [] });

  const session = await client.GET("/api/v2/contexts/{contextId}/session", {
    params: { path: { contextId } },
  });
  assert.equal(session.data?.source, "stdin");
  assert.deepEqual(session.data?.capabilities, {
    diff: {
      scopes: { state: "enabled", values: [apiValues.allScope] },
      refresh: { state: "unavailable" },
      stagingMetadata: { state: "unavailable" },
    },
    files: { contents: { state: "unavailable" } },
    review: { comments: { state: "enabled" } },
  });

  const diff = await client.GET("/api/v2/contexts/{contextId}/diffs/current", {
    params: { path: { contextId }, query: { scope: apiValues.allScope } },
  });
  assert.ok(diff.data);
  conforms("RepositoryDiff", diff.data);
  assert.equal(session.data?.name, diff.data.name);
  assert.equal(diff.data.files.length, 12);
  const file = diff.data.files.find(
    (candidate) => candidate.path === "src/value.ts",
  );
  assert.ok(file);

  const preview = await client.GET(
    "/api/v2/contexts/{contextId}/diffs/{diffId}/files/{fileId}/patch",
    {
      params: {
        path: { contextId, diffId: diff.data.id, fileId: file.id },
        query: {
          scope: apiValues.allScope,
          versionId: diff.data.versionId,
          fileVersion: file.fingerprint,
        },
      },
    },
  );
  assert.match(preview.data?.patch ?? "", /export const value = 2/);

  const created = await client.POST("/api/v2/contexts/{contextId}/comments", {
    params: { path: { contextId } },
    body: {
      diffId: diff.data.id,
      versionId: diff.data.versionId,
      fileId: file.id,
      scope: apiValues.allScope,
      fileVersion: file.fingerprint,
      side: "additions",
      start: 1,
      end: 1,
      body: "Keep the new value.",
    },
  });
  conforms("ReviewComment", created.data);
  conforms("FilePatch", preview.data);
  assert.equal(created.data?.path, file.path);
  assert.equal(created.data?.code, "+ export const value = 2;");
  assert.equal(created.data?.body, "Keep the new value.");

  for (const path of [
    "/api/v1/session",
    "/api/v1/diffs/current",
    "/api/v2/ingestions",
  ]) {
    const response = await fetch(url + path);
    assert.equal(response.status, 404);
  }

  const patches: Record<string, components["schemas"]["FilePatch"]> = {};
  for (const observedFile of diff.data.files) {
    const result: { data?: components["schemas"]["FilePatch"] } =
      await client.GET(
        "/api/v2/contexts/{contextId}/diffs/{diffId}/files/{fileId}/patch",
        {
          params: {
            path: {
              contextId,
              diffId: diff.data.id,
              fileId: observedFile.id,
            },
            query: {
              scope: apiValues.allScope,
              versionId: diff.data.versionId,
              fileVersion: observedFile.fingerprint,
            },
          },
        },
      );
    assert.ok(result.data);
    patches[observedFile.id] = result.data;
  }
  const body = {
    protocolVersion: 4,
    submissionId: "contract-observation",
    metadata: {
      sourceId: "sandbox-contract-source",
      hostname: "sandbox-host",
      runId: "agent-run",
      agent: "contract-agent",
      trigger: "agent-hook",
      repositoryKey: "contract-repository",
      repositoryName: "contract-repo",
      remoteUrl: "",
      checkoutKey: "sandbox-checkout",
      root: "/workspace/repo",
      worktreeName: "sandbox-worktree",
      branch: "agent-branch",
      head: null,
      collectedAt: Date.now(),
      collectorVersion: "conformance",
    },
    scopes: [
      {
        snapshot: {
          ...diff.data,
          root: "/workspace/repo",
          name: "contract-repo",
          branch: "agent-branch",
          head: null,
        },
        patches,
      },
    ],
  } satisfies components["schemas"]["IngestionRequest"];
  const localJob = await client.POST("/api/v2/ingestion-jobs", { body });
  assert.equal(localJob.response.status, 404);

  const remotePort = await freePort();
  const databaseUrl = await serverDatabase(t);
  const remoteServer = spawn(
    remoteBinary,
    ["--listen", `127.0.0.1:${remotePort}`],
    {
      stdio: ["ignore", "ignore", "pipe"],
      env: {
        ...process.env,
        DIFFX_SERVER_DATABASE_URL: databaseUrl,
        // Loopback HTTP enables Web Push for development.
        DIFFX_SERVER_PUBLIC_URL: `http://localhost:${remotePort}`,
        DIFFX_SERVER_INSECURE_COOKIES: "true",
      },
    },
  );
  t.after(() => remoteServer.kill("SIGTERM"));
  let serverLog = "";
  remoteServer.stderr.setEncoding("utf8");
  remoteServer.stderr.on("data", (chunk: string) => (serverLog += chunk));
  const remoteUrl = `http://127.0.0.1:${remotePort}`;
  await waitForServer(remoteUrl);
  const remote = createApiClient({ baseUrl: remoteUrl });
  for (const probe of [
    await remote.GET("/healthz"),
    await remote.GET("/readyz"),
  ]) {
    assert.equal(probe.response.status, 200);
    assert.equal(probe.response.headers.get("cache-control"), "no-store");
    conforms("Probe", probe.data);
  }
  const remoteBootstrap = await remote.GET("/api/v2/bootstrap");
  conforms("Bootstrap", remoteBootstrap.data);
  assert.deepEqual(remoteBootstrap.data, {
    mode: "remote",
    googleEnabled: false,
    user: null,
    csrfToken: null,
  });
  const retiredSignup = await fetch(`${remoteUrl}/api/v2/auth/signup`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      username: "second-user",
      password: "contract-password-000002",
    }),
  });
  assert.equal(retiredSignup.status, 401);
  const pendingSetup = await remote.GET("/api/v2/setup");
  assert.deepEqual(pendingSetup.data, { required: true });
  let setupToken = "";
  for (let attempt = 0; !setupToken && attempt < 40; attempt++) {
    setupToken =
      /http:\/\/localhost:\d+\/setup#t=(\S+)/.exec(serverLog)?.[1] ?? "";
    if (!setupToken) await new Promise((resolve) => setTimeout(resolve, 50));
  }
  assert.ok(setupToken, `setup link not logged: ${serverLog}`);
  const setup = await remote.POST("/api/v2/setup", {
    body: {
      token: setupToken,
      username: "contract-user",
      password: "contract-password-000001",
    },
  });
  assert.equal(setup.response.status, 200);
  conforms("Bootstrap", setup.data);
  assert.equal(setup.data?.mode === "remote" && setup.data.user?.admin, true);
  const reusedSetup = await remote.POST("/api/v2/setup", {
    body: {
      token: setupToken,
      username: "second-user",
      password: "contract-password-000002",
    },
  });
  assert.equal(reusedSetup.response.status, 404);
  const login = await remote.POST("/api/v2/auth/login", {
    body: {
      username: "contract-user",
      password: "contract-password-000001",
    },
  });
  assert.equal(login.response.status, 200);
  conforms("Bootstrap", login.data);
  assert.ok(login.data?.mode === "remote");
  assert.equal(login.data?.user?.username, "contract-user");
  const csrf = login.data?.csrfToken;
  assert.ok(csrf);
  const cookie = login.response.headers.get("set-cookie")?.split(";")[0];
  assert.ok(cookie);
  const accountHeaders = { Cookie: cookie, "X-CSRF-Token": csrf };
  const users = await remote.GET("/api/v2/admin/users", {
    headers: { Cookie: cookie },
  });
  assert.equal(users.response.status, 200);
  assert.equal(users.data?.users.length, 1);
  conforms("AdminUser", users.data?.users[0]);
  const invites = await remote.GET("/api/v2/admin/invites", {
    headers: { Cookie: cookie },
  });
  assert.equal(invites.response.status, 200);
  assert.deepEqual(invites.data?.invites, []);
  const browserKey = createECDH("prime256v1");
  const pushDevice = await remote.PUT("/api/v2/push/subscriptions", {
    headers: accountHeaders,
    body: {
      endpoint: "https://push.example/send/contract",
      keys: {
        p256dh: browserKey.generateKeys().toString("base64url"),
        auth: randomBytes(16).toString("base64url"),
      },
      label: "Contract browser",
    },
  });
  assert.equal(pushDevice.response.status, 200);
  conforms("PushDevice", pushDevice.data);
  assert.equal(pushDevice.data?.lastSuccessAt, null);
  const pushSettings = await remote.GET("/api/v2/push", {
    headers: { Cookie: cookie },
  });
  assert.equal(pushSettings.response.status, 200);
  conforms("PushSettings", pushSettings.data);
  assert.equal(pushSettings.data?.enabled, true);
  assert.equal(pushSettings.data?.devices.length, 1);
  const inviteWithoutGoogle = await remote.POST("/api/v2/admin/invites", {
    headers: accountHeaders,
    body: { email: "member@example.com" },
  });
  assert.equal(inviteWithoutGoogle.response.status, 409);
  const googleWithoutConfig = await remote.POST("/api/v2/auth/google", {
    body: {},
  });
  assert.equal(googleWithoutConfig.response.status, 404);
  const missingCSRF = await remote.POST("/api/v2/account/tokens", {
    headers: { Cookie: cookie },
    body: { name: "Denied", purpose: "api", expiresInDays: 90 },
  });
  assert.equal(missingCSRF.response.status, 403);
  const createdToken = await remote.POST("/api/v2/account/tokens", {
    headers: accountHeaders,
    body: { name: "Contract API", purpose: "api", expiresInDays: 90 },
  });
  assert.equal(createdToken.response.status, 201);
  conforms("AccountToken", createdToken.data?.token);
  assert.ok(createdToken.data?.secret);
  const token = createdToken.data.secret;
  const collectorToken = await remote.POST("/api/v2/account/tokens", {
    headers: accountHeaders,
    body: {
      name: "Contract collector",
      purpose: "collector",
      expiresInDays: 90,
    },
  });
  assert.equal(collectorToken.response.status, 201);
  conforms("AccountToken", collectorToken.data?.token);
  assert.ok(collectorToken.data?.secret);
  const collectorSecret = collectorToken.data.secret;
  const collector = createApiClient({ baseUrl: remoteUrl });
  collector.use({
    onRequest({ request }) {
      request.headers.set("Authorization", `Bearer ${collectorSecret}`);
      return request;
    },
  });
  const unauthenticated = await remote.GET("/api/v2/health");
  assert.equal(unauthenticated.response.status, 401);
  remote.use({
    onRequest({ request }) {
      request.headers.set("Authorization", `Bearer ${token}`);
      return request;
    },
  });
  const remoteHealth = await remote.GET("/api/v2/health");
  assert.equal(remoteHealth.data?.ingestionEnabled, true);
  assert.equal(remoteHealth.data?.queuedIngestion, true);
  const bearerAccount = await remote.GET("/api/v2/account/tokens");
  assert.equal(bearerAccount.response.status, 403);
  const ingested = await collector.POST("/api/v2/ingestion-jobs", { body });
  assert.equal(ingested.response.status, 202);
  assert.ok(ingested.data);
  conforms("IngestionJob", ingested.data);
  let job = ingested.data;
  for (let attempt = 0; job.state !== "succeeded" && attempt < 80; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 50));
    const result = await remote.GET("/api/v2/ingestion-jobs/{jobId}", {
      params: { path: { jobId: job.id } },
    });
    assert.ok(result.data);
    job = result.data;
  }
  conforms("IngestionJob", job);
  assert.equal(job.state, "succeeded");
  assert.ok(job.contextId);
  assert.notEqual(job.contextId, contextId);
  const reconciliation = await collector.GET("/api/v2/contexts/{contextId}", {
    params: { path: { contextId: job.contextId } },
  });
  assert.equal(reconciliation.response.status, 200);
  const collectorReview = await collector.GET(
    "/api/v2/contexts/{contextId}/comments",
    {
      params: { path: { contextId: job.contextId } },
    },
  );
  assert.equal(collectorReview.response.status, 403);
  const replay = await remote.POST("/api/v2/ingestion-jobs", { body });
  assert.deepEqual(replay.data, job);

  const filtered = await remote.GET("/api/v2/contexts", {
    params: {
      query: {
        repository: "contract-repo",
        branch: "agent-branch",
        worktree: "sandbox-worktree",
        hostname: "sandbox-host",
        sourceId: "sandbox-contract-source",
        runId: "agent-run",
      },
    },
  });
  assert.equal(filtered.data?.contexts.length, 1);
  assert.equal(filtered.data?.contexts[0]?.id, job.contextId);
  assert.deepEqual(filtered.data?.contexts[0]?.observation, body.metadata);

  const conflict = await remote.POST("/api/v2/ingestion-jobs", {
    body: {
      ...body,
      metadata: { ...body.metadata, hostname: "different-host" },
    },
  });
  assert.equal(conflict.response.status, 409);
  const revoked = await createApiClient({ baseUrl: remoteUrl }).DELETE(
    "/api/v2/account/tokens/{tokenId}",
    {
      headers: accountHeaders,
      params: { path: { tokenId: collectorToken.data.token.id } },
    },
  );
  assert.equal(revoked.response.status, 204);
  const revokedHealth = await collector.GET("/api/v2/health");
  assert.equal(revokedHealth.response.status, 401);
});
