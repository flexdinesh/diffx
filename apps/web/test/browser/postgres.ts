import { randomBytes } from "node:crypto";
import { PostgreSqlContainer } from "@testcontainers/postgresql";
import pg from "pg";

// Matches test/conformance/postgres.ts: DIFFX_TEST_DATABASE_URL selects a
// running server whose role may create databases; otherwise each worker starts
// the pinned image and stops it with its database.
const image =
  "postgres:18-alpine@sha256:10a5c69a83035ab5239f5620d708520e8b28f5aa1f2b20143e6024532edadfb5";

async function adminServer(): Promise<{
  admin: URL;
  stop: () => Promise<void>;
}> {
  const configured = process.env.DIFFX_TEST_DATABASE_URL;
  if (configured) return { admin: new URL(configured), stop: async () => {} };
  const container = await new PostgreSqlContainer(image)
    .withDatabase("diffx")
    .withUsername("diffx")
    .withPassword("diffx")
    .start();
  return {
    admin: new URL(container.getConnectionUri()),
    stop: async () => {
      await container.stop();
    },
  };
}

async function withAdmin(
  admin: URL,
  run: (client: pg.Client) => Promise<unknown>,
) {
  const client = new pg.Client({ connectionString: admin.href });
  await client.connect();
  try {
    await run(client);
  } finally {
    await client.end();
  }
}

/** Creates an empty database for diffx-server; `drop` removes it. */
export async function createServerDatabase(): Promise<{
  url: string;
  drop: () => Promise<void>;
}> {
  const { admin, stop } = await adminServer();
  const name = `diffx_browser_${randomBytes(8).toString("hex")}`;
  try {
    await withAdmin(admin, (client) => client.query(`CREATE DATABASE ${name}`));
  } catch (error) {
    await stop();
    throw error;
  }
  const url = new URL(admin.href);
  url.pathname = `/${name}`;
  url.searchParams.set("sslmode", "disable");
  return {
    url: url.href,
    drop: async () => {
      try {
        await withAdmin(admin, (client) =>
          client.query(`DROP DATABASE IF EXISTS ${name} WITH (FORCE)`),
        );
      } finally {
        await stop();
      }
    },
  };
}
