import assert from "node:assert/strict";
import {
  type ChildProcess,
  execFile,
  execFileSync,
  spawn,
} from "node:child_process";
import { createECDH, createHash, randomBytes } from "node:crypto";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import pg from "pg";
import { createApiClient } from "../../packages/api/src/client.ts";
import type { components } from "../../packages/api/src/schema.d.ts";
import { serverDatabase } from "./postgres.ts";
import { conforms, freePort, waitForServer } from "./support.ts";

const binary = fileURLToPath(new URL("../../dist/diffx", import.meta.url));
const remoteBinary = fileURLToPath(
  new URL("../../dist/diffx-server", import.meta.url),
);
const run = promisify(execFile);
const patience = 15_000;

/** Polls read until it yields a value, failing after the deadline. */
async function until<T>(
  read: () => Promise<T | undefined>,
  message: string,
): Promise<T> {
  const deadline = Date.now() + patience;
  for (;;) {
    const value = await read();
    if (value !== undefined) return value;
    if (Date.now() > deadline) throw new Error(message);
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
}

/** Settles with promise, or rejects once the deadline passes. */
async function within<T>(promise: Promise<T>, message: string): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  const expired = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(message)), patience);
  });
  try {
    return await Promise.race([promise, expired]);
  } finally {
    clearTimeout(timer);
  }
}

type Replica = { url: string; log: () => string; process: ChildProcess };

/** Resolves once the process exits; SIGTERM is diffx-server's graceful stop. */
function stop(child: ChildProcess) {
  return new Promise<number | null>((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) {
      resolve(child.exitCode);
      return;
    }
    child.once("exit", (code) => resolve(code));
    child.kill("SIGTERM");
  });
}

async function startReplica(
  port: number,
  databaseUrl: string,
  publicUrl: string,
  replicas: Replica[],
): Promise<Replica> {
  const child = spawn(remoteBinary, ["--listen", `127.0.0.1:${port}`], {
    stdio: ["ignore", "ignore", "pipe"],
    env: {
      ...process.env,
      DIFFX_SERVER_DATABASE_URL: databaseUrl,
      // One deployment origin; loopback HTTP enables Web Push for development.
      DIFFX_SERVER_PUBLIC_URL: publicUrl,
      DIFFX_SERVER_INSECURE_COOKIES: "true",
    },
  });
  let log = "";
  child.stderr?.setEncoding("utf8");
  child.stderr?.on("data", (chunk: string) => (log += chunk));
  const replica = {
    url: `http://127.0.0.1:${port}`,
    log: () => log,
    process: child,
  };
  replicas.push(replica);
  await waitForServer(replica.url);
  return replica;
}

type ServerEvent = { event: string; data: string };

/** Reads server-sent events; next() is undefined once the server ends the stream. */
async function openEvents(url: string, token: string) {
  const response = await fetch(`${url}/api/v2/events`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  assert.equal(response.status, 200);
  assert.match(
    response.headers.get("content-type") ?? "",
    /^text\/event-stream/,
  );
  const body = response.body;
  assert.ok(body);
  const reader = body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  const next = async (): Promise<ServerEvent | undefined> => {
    for (;;) {
      const end = buffer.indexOf("\n\n");
      if (end >= 0) {
        const block = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        const event: ServerEvent = { event: "", data: "" };
        for (const line of block.split("\n")) {
          if (line.startsWith(":")) event.event ||= "comment";
          else if (line.startsWith("event: ")) event.event = line.slice(7);
          else if (line.startsWith("data: ")) event.data += line.slice(6);
        }
        return event;
      }
      const chunk = await reader.read();
      if (chunk.done) return undefined;
      buffer += chunk.value;
    }
  };
  // The server writes this comment after subscribing, so later commits reach the stream.
  assert.equal(
    (await within(next(), "events did not connect"))?.event,
    "comment",
  );
  return {
    next,
    /** Resolves to the next published context's ID; resync hints carry none. */
    async ingestion(): Promise<string> {
      for (;;) {
        const event = await next();
        assert.ok(event, "event stream ended");
        if (event.event !== "ingestion") continue;
        const value: unknown = JSON.parse(event.data);
        assert.ok(
          typeof value === "object" && value !== null && "contextId" in value,
        );
        assert.equal(typeof value.contextId, "string");
        if (typeof value.contextId === "string" && value.contextId)
          return value.contextId;
      }
    },
  };
}

type IngestionRequest = components["schemas"]["IngestionRequest"];

/** A piped capture whose branch names its own review stream. */
function capture(submissionId: string, branch: string): IngestionRequest {
  const file = {
    id: "file-value",
    path: "src/value.ts",
    oldPath: null,
    status: "modified",
    indexStatus: " ",
    worktreeStatus: "M",
    additions: 1,
    deletions: 1,
    binary: false,
    fingerprint: `fingerprint-${branch}`,
  };
  return {
    protocolVersion: 4,
    submissionId,
    metadata: {
      sourceId: "replica-source",
      hostname: "replica-host",
      runId: "replica-run",
      agent: "replica-agent",
      trigger: "pipe",
      repositoryKey: "replica-repository",
      repositoryName: "replica-repo",
      remoteUrl: "",
      checkoutKey: "replica-checkout",
      root: "",
      worktreeName: "",
      branch,
      head: null,
      collectedAt: Date.now(),
      collectorVersion: "conformance",
    },
    scopes: [
      {
        snapshot: {
          id: `diff-${branch}`,
          versionId: `version-${branch}`,
          locationId: null,
          repositoryId: null,
          source: "stdin",
          root: "",
          name: "replica-repo",
          branch,
          head: null,
          mode: "all",
          comparisonKind: "patch",
          files: [file],
          revision: `revision-${branch}`,
        },
        patches: {
          [file.id]: {
            patch: `--- a/src/value.ts\n+++ b/src/value.ts\n@@ -1 +1 @@\n-export const value = 1;\n+export const value = "${branch}";\n`,
            message: null,
          },
        },
      },
    ],
  };
}

test("Replicas sharing one database act as one server", async (t) => {
  const replicas: Replica[] = [];
  const sessions: pg.Client[] = [];
  // Registered before the database, so its clients leave before it is dropped.
  t.after(async () => {
    await Promise.all(replicas.map((replica) => stop(replica.process)));
    await Promise.all(sessions.map((session) => session.end()));
  });
  const databaseUrl = await serverDatabase(t);
  const database = new pg.Client({ connectionString: databaseUrl });
  sessions.push(database);
  await database.connect();
  const home = mkdtempSync(join(tmpdir(), "diffx-replicas-"));
  t.after(() => rmSync(home, { recursive: true, force: true }));

  const [portA, portB] = [await freePort(), await freePort()];
  const publicUrl = `http://localhost:${portA}`;
  const a = await startReplica(portA, databaseUrl, publicUrl, replicas);
  const b = await startReplica(portB, databaseUrl, publicUrl, replicas);
  const clientA = createApiClient({ baseUrl: a.url });
  const clientB = createApiClient({ baseUrl: b.url });

  await t.test("both replicas answer deployment probes", async () => {
    for (const client of [clientA, clientB]) {
      for (const probe of [
        await client.GET("/healthz"),
        await client.GET("/readyz"),
      ]) {
        assert.equal(probe.response.status, 200);
        assert.equal(probe.response.headers.get("cache-control"), "no-store");
        conforms("Probe", probe.data);
      }
    }
  });

  // Each replica issues a setup secret on start; the latest replaces earlier ones.
  const setupToken = await until(
    async () => /\/setup#t=(\S+)/.exec(b.log())?.[1],
    `setup link not logged: ${b.log()}`,
  );
  const adminPassword = "replica-password-000001";
  const setup = await clientA.POST("/api/v2/setup", {
    body: {
      token: setupToken,
      username: "replica-admin",
      password: adminPassword,
    },
  });
  assert.equal(setup.response.status, 200);
  conforms("Bootstrap", setup.data);
  assert.ok(setup.data?.mode === "remote" && setup.data.user?.admin);
  const adminId = setup.data.user.id;
  assert.deepEqual((await clientB.GET("/api/v2/setup")).data, {
    required: false,
  });

  /** Signs the admin in through one replica; returns its browser session headers. */
  async function signIn(client: typeof clientA) {
    const login = await client.POST("/api/v2/auth/login", {
      body: { username: "replica-admin", password: adminPassword },
    });
    assert.equal(login.response.status, 200);
    conforms("Bootstrap", login.data);
    assert.ok(login.data?.mode === "remote" && login.data.csrfToken);
    const cookie = login.response.headers.get("set-cookie")?.split(";")[0];
    assert.ok(cookie);
    return { Cookie: cookie, "X-CSRF-Token": login.data.csrfToken };
  }
  async function apiToken(
    client: typeof clientA,
    headers: Record<string, string>,
  ) {
    const created = await client.POST("/api/v2/account/tokens", {
      headers,
      body: { name: "Replica API", purpose: "api", expiresInDays: 1 },
    });
    assert.equal(created.response.status, 201);
    conforms("AccountToken", created.data?.token);
    assert.ok(created.data?.secret);
    return created.data.secret;
  }
  function bearer(url: string, token: string) {
    const client = createApiClient({ baseUrl: url });
    client.use({
      onRequest({ request }) {
        request.headers.set("Authorization", `Bearer ${token}`);
        return request;
      },
    });
    return client;
  }

  // A session started on B administers through A.
  const admin = await signIn(clientB);
  // Members exist only through Google invites and sign in only with Google,
  // which a test cannot complete. Seed what that sign-in commits: an account
  // without a password and one browser session (stored as the SHA-256 of its
  // 32-byte secret). The member then creates its API token over HTTP.
  const memberId = `replica-member-${randomBytes(8).toString("hex")}`;
  const memberSecret = randomBytes(32).toString("base64url");
  const memberCSRF = randomBytes(32).toString("base64url");
  await database.query(
    `WITH account AS (
       INSERT INTO diffx.users(id, name, created_at) VALUES ($1, 'replica-member', now())
       RETURNING id
     ), credential AS (
       INSERT INTO diffx.remote_accounts(user_id, username, password_hash)
       SELECT id, 'replica-member', '' FROM account RETURNING user_id
     )
     INSERT INTO diffx.browser_sessions(secret_hash, user_id, csrf_token, expires_at)
     SELECT $2, user_id, $3, now() + interval '1 day' FROM credential`,
    [
      memberId,
      createHash("sha256").update(memberSecret).digest("hex"),
      memberCSRF,
    ],
  );
  const member = {
    Cookie: `diffx_session=${memberSecret}`,
    "X-CSRF-Token": memberCSRF,
  };
  const memberToken = await apiToken(clientA, member);
  const memberA = bearer(a.url, memberToken);
  const memberB = bearer(b.url, memberToken);
  const users = await clientA.GET("/api/v2/admin/users", { headers: admin });
  assert.equal(users.response.status, 200);
  for (const user of users.data?.users ?? []) conforms("AdminUser", user);
  assert.equal(users.data?.users.length, 2);
  const seeded = users.data?.users.find((user) => user.id === memberId);
  assert.equal(seeded?.admin, false);
  assert.equal(seeded?.disabled, false);

  const eventsB = await openEvents(b.url, memberToken);

  await t.test("a capture synced through A streams from B", async () => {
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
    writeFileSync(join(root, "file.txt"), "after\n");
    await run(binary, ["sync", "--path", root], {
      env: {
        ...process.env,
        DIFFX_RUNTIME_DIR: join(home, "runtime"),
        DIFFX_CONFIG_PATH: join(home, "config.json"),
        DIFFX_SOURCE_ID: "replica-sync",
        DIFFX_REMOTE_URL: a.url,
        DIFFX_REMOTE_TOKEN: memberToken,
      },
    });
    const contextId = await within(
      eventsB.ingestion(),
      "B streamed no ingestion",
    );
    const context = await memberB.GET("/api/v2/contexts/{contextId}", {
      params: { path: { contextId } },
    });
    assert.equal(context.response.status, 200);
    conforms("Context", context.data);
    assert.equal(context.data?.observation?.sourceId, "replica-sync");
    assert.equal(context.data?.observation?.branch, "main");
  });

  await t.test("a job queued on A completes and reads on B", async () => {
    const admitted = await memberA.POST("/api/v2/ingestion-jobs", {
      body: capture("replica-job", "replica-job"),
    });
    assert.equal(admitted.response.status, 202);
    conforms("IngestionJob", admitted.data);
    assert.ok(admitted.data);
    const jobId = admitted.data.id;
    const job = await until(async () => {
      const polled = await memberB.GET("/api/v2/ingestion-jobs/{jobId}", {
        params: { path: { jobId } },
      });
      assert.equal(polled.response.status, 200);
      conforms("IngestionJob", polled.data);
      return polled.data?.state === "succeeded" ? polled.data : undefined;
    }, "job did not succeed");
    const contextId = job.contextId;
    assert.ok(contextId);
    await within(
      (async () => {
        while ((await eventsB.ingestion()) !== contextId);
      })(),
      "B streamed no ingestion of the job's context",
    );
    const context = await memberB.GET("/api/v2/contexts/{contextId}", {
      params: { path: { contextId } },
    });
    assert.equal(context.response.status, 200);
    conforms("Context", context.data);
    assert.equal(context.data?.observation?.branch, "replica-job");
    const diff = await memberB.GET(
      "/api/v2/contexts/{contextId}/diffs/current",
      {
        params: { path: { contextId }, query: { scope: "all" } },
      },
    );
    assert.equal(diff.response.status, 200);
    conforms("RepositoryDiff", diff.data);
    assert.deepEqual(
      diff.data?.files.map((file) => file.path),
      ["src/value.ts"],
    );
  });

  await t.test("disabling the member through A cuts it off on B", async () => {
    const disabled = await clientA.POST(
      "/api/v2/admin/users/{userId}/disable",
      {
        headers: admin,
        params: { path: { userId: memberId } },
      },
    );
    assert.equal(disabled.response.status, 204);
    await within(
      (async () => {
        while (await eventsB.next());
      })(),
      "B kept the disabled member's event stream open",
    );
    assert.equal((await memberB.GET("/api/v2/health")).response.status, 401);
    const events = await fetch(`${b.url}/api/v2/events`, {
      headers: { Authorization: `Bearer ${memberToken}` },
    });
    assert.equal(events.status, 401);
    const session = await clientB.GET("/api/v2/bootstrap", {
      headers: { Cookie: member.Cookie },
    });
    assert.ok(session.data?.mode === "remote");
    assert.equal(session.data.user, null);
    const users = await clientB.GET("/api/v2/admin/users", { headers: admin });
    assert.equal(
      users.data?.users.find((user) => user.id === memberId)?.disabled,
      true,
    );
  });

  await t.test(
    "replicas deliver each push notification once per device",
    async () => {
      const received = new Map<string, number>();
      // A slow push service keeps each delivery in flight across the other
      // replica's polls, so both workers contend for every notification.
      const pushService = createServer((request, response) => {
        request.resume();
        request.on("end", () => {
          const path = request.url ?? "";
          received.set(path, (received.get(path) ?? 0) + 1);
          setTimeout(() => response.writeHead(201).end(), 250);
        });
      });
      await new Promise<void>((resolve) =>
        pushService.listen(0, "127.0.0.1", resolve),
      );
      t.after(() => new Promise((resolve) => pushService.close(resolve)));
      const address = pushService.address();
      assert.ok(address && typeof address !== "string");
      const pushOrigin = `http://127.0.0.1:${address.port}`;
      // Each replica registers one of the admin's devices.
      const devices = [
        { client: clientA, path: "/send/first" },
        { client: clientB, path: "/send/second" },
      ];
      const endpoints = devices.map((device) => device.path);
      for (const [index, { client, path }] of devices.entries()) {
        const registered = await client.PUT("/api/v2/push/subscriptions", {
          headers: admin,
          body: {
            endpoint: pushOrigin + path,
            keys: {
              p256dh: createECDH("prime256v1")
                .generateKeys()
                .toString("base64url"),
              auth: randomBytes(16).toString("base64url"),
            },
            label: `Replica device ${index + 1}`,
          },
        });
        assert.equal(registered.response.status, 200);
        conforms("PushDevice", registered.data);
      }
      const adminToken = await apiToken(clientA, admin);
      const admitted = await bearer(a.url, adminToken).POST(
        "/api/v2/ingestion-jobs",
        {
          body: capture("replica-push", "replica-push"),
        },
      );
      assert.equal(admitted.response.status, 202);
      assert.ok(admitted.data);
      const jobId = admitted.data.id;
      await until(async () => {
        const polled = await bearer(b.url, adminToken).GET(
          "/api/v2/ingestion-jobs/{jobId}",
          {
            params: { path: { jobId } },
          },
        );
        return polled.data?.state === "succeeded" ? true : undefined;
      }, "push job did not succeed");
      // Only the GitHub integration records notification intent, and it needs
      // GitHub. Record it as that admission does: pending rows linked to the
      // admin's succeeded job, which both replicas' workers then compete for.
      const notifications = 6;
      await database.query(
        `INSERT INTO diffx.push_notifications(id, owner_id, job_id, kind, payload, state, available_at, created_at)
       SELECT 'replica-push-' || n, $1, $2, 'pull_request', convert_to($3, 'UTF8'), 'pending', now(), now()
       FROM generate_series(1, $4::integer) n`,
        [
          adminId,
          jobId,
          JSON.stringify({
            title: "replica/repo #1",
            body: "Replicas\nUpdated by @replica",
          }),
          notifications,
        ],
      );
      const settled = await until(async () => {
        const states = await database.query<{ state: string }>(
          "SELECT state FROM diffx.push_notifications WHERE job_id = $1",
          [jobId],
        );
        const values = states.rows.map((row) => row.state);
        return values.includes("pending") ? undefined : values;
      }, "replicas did not finish delivering");
      assert.deepEqual(
        settled,
        Array.from({ length: notifications }, () => "sent"),
      );
      // Stopping both replicas joins their delivery workers, so every send has landed.
      const exits = await Promise.all(
        replicas.map((replica) => stop(replica.process)),
      );
      assert.deepEqual(exits, [0, 0]);
      assert.deepEqual(
        Object.fromEntries(received),
        Object.fromEntries(endpoints.map((path) => [path, notifications])),
      );
    },
  );
});
