import { randomBytes } from "node:crypto";
import type { TestContext } from "node:test";
import { PostgreSqlContainer } from "@testcontainers/postgresql";
import pg from "pg";

// Matches internal/testsupport/pgtest: DIFFX_TEST_DATABASE_URL selects a running
// server whose role may create databases; otherwise one container starts per
// test process and testcontainers' reaper removes it.
const image =
  "postgres:18-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5";

let admin: Promise<URL> | undefined;

function adminURL(): Promise<URL> {
  admin ??= (async () => {
    const configured = process.env.DIFFX_TEST_DATABASE_URL;
    if (configured) return new URL(configured);
    const container = await new PostgreSqlContainer(image)
      .withDatabase("diffx")
      .withUsername("diffx")
      .withPassword("diffx")
      .start();
    return new URL(container.getConnectionUri());
  })();
  return admin;
}

async function withAdmin(run: (client: pg.Client) => Promise<unknown>) {
  const client = new pg.Client({ connectionString: (await adminURL()).href });
  await client.connect();
  try {
    await run(client);
  } finally {
    await client.end();
  }
}

/** Returns an empty database for diffx-server, dropped when the test ends. */
export async function serverDatabase(t: TestContext): Promise<string> {
  const name = `diffx_conformance_${randomBytes(8).toString("hex")}`;
  await withAdmin((client) => client.query(`CREATE DATABASE ${name}`));
  t.after(() =>
    withAdmin((client) =>
      client.query(`DROP DATABASE IF EXISTS ${name} WITH (FORCE)`),
    ),
  );
  const url = new URL((await adminURL()).href);
  url.pathname = `/${name}`;
  url.searchParams.set("sslmode", "disable");
  return url.href;
}
