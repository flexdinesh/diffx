import { type ChildProcessByStdio, spawn } from "node:child_process";
import { createInterface } from "node:readline";
import type { Readable } from "node:stream";
import { expect, type Page, test as base } from "@playwright/test";
import { createServerDatabase } from "./postgres.ts";

export { expect };

type Deployment = {
  /** The console origin, the test's baseURL. */
  origin: string;
  /** The diffx app origin invite and member reset links open. */
  app: string;
  /** The one-time setup link diffx-console logged at startup. */
  setup: string;
};

/** Built by global-setup.ts or supplied by full checks. */
type Binaries = { server: string | undefined; console: string | undefined };

type Child = ChildProcessByStdio<null, Readable, Readable>;

/** Resolves the address a binary reports once it serves. */
function listening(name: string, child: Child) {
  return new Promise<string>((resolve, reject) => {
    const timer = setTimeout(
      () => reject(new Error(`Timed out starting ${name}`)),
      60_000,
    );
    child.once("error", reject);
    child.once("exit", (code, signal) =>
      reject(
        new Error(`${name} exited before startup (${signal ?? code ?? "?"})`),
      ),
    );
    createInterface({ input: child.stdout }).on("line", (line) => {
      const address = /listening on (\S+)/.exec(line)?.[1];
      if (!address) return;
      clearTimeout(timer);
      resolve(address);
    });
  });
}

async function stop(child: Child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  const exited = new Promise<void>((resolve) =>
    child.once("close", () => resolve()),
  );
  child.kill("SIGTERM");
  await exited;
}

/**
 * Each worker owns diffx-server and diffx-console on one fresh Postgres
 * database, as deployments run them: two origins over one database. The
 * console serves its embedded build (staged by `mise run build`).
 */
export const test = base.extend<
  object,
  { binaries: Binaries; deployment: Deployment }
>({
  // Workers start after global setup, so its build paths are in the environment.
  binaries: [
    {
      server: process.env.DIFFX_FIXTURE_SERVER_BINARY,
      console: process.env.DIFFX_FIXTURE_CONSOLE_BINARY,
    },
    { scope: "worker", option: true },
  ],
  deployment: [
    async ({ binaries }, use) => {
      const { server: serverBinary, console: consoleBinary } = binaries;
      if (!serverBinary || !consoleBinary)
        throw new Error("Missing diffx-server or diffx-console build");
      const database = await createServerDatabase();
      const children: Child[] = [];
      try {
        const server = spawn(serverBinary, ["--listen", "127.0.0.1:0"], {
          env: {
            ...process.env,
            DIFFX_SERVER_DATABASE_URL: database.url,
            DIFFX_SERVER_INSECURE_COOKIES: "true",
          },
          stdio: ["ignore", "pipe", "pipe"],
        });
        children.push(server);
        server.stderr.on("data", (chunk) => process.stderr.write(chunk));
        const app = `http://${await listening("diffx-server", server)}`;
        const console = spawn(consoleBinary, [], {
          env: {
            ...process.env,
            DIFFX_CONSOLE_DATABASE_URL: database.url,
            DIFFX_CONSOLE_PORT: "0",
            DIFFX_CONSOLE_APP_URL: app,
            DIFFX_CONSOLE_INSECURE_COOKIES: "true",
          },
          stdio: ["ignore", "pipe", "pipe"],
        });
        children.push(console);
        // The setup link is logged to stderr, which may trail stdout.
        const setup = new Promise<string>((resolve) => {
          createInterface({ input: console.stderr }).on("line", (line) => {
            process.stderr.write(`${line}\n`);
            const link = /(\S+\/setup#t=\S+)/.exec(line)?.[1];
            if (link) resolve(link);
          });
        });
        const origin = `http://${await listening("diffx-console", console)}`;
        await use({ origin, app, setup: await setup });
      } finally {
        await Promise.all(children.map(stop));
        await database.drop();
      }
    },
    { scope: "worker" },
  ],
  baseURL: async ({ deployment }, use) => use(deployment.origin),
});

export const root = {
  email: "root.with.a.long.email.address@example.com",
  password: "console-password-0001",
};

export async function signIn(
  page: Page,
  { email, password }: { email: string; password: string },
) {
  await page.goto("/");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}
