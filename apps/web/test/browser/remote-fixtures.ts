import { spawn } from "node:child_process";
import { createInterface } from "node:readline";
import { createServerDatabase } from "./postgres.ts";
import { test as base } from "./fixtures.ts";

export { expect } from "./fixtures.ts";

type RemoteServer = { origin: string };

/**
 * Each worker owns one diffx-server on its own Postgres database. The server
 * serves its embedded web build (staged from apps/web/dist by `mise run build`)
 * at its own origin, as deployments do, so sessions, CSRF and SPA routing are
 * the server's own.
 */
export const test = base.extend<object, { remoteServer: RemoteServer }>({
  remoteServer: [
    async ({ fixtureBuild: { serverBinary } }, use) => {
      if (!serverBinary) throw new Error("Missing diffx-server build");
      const database = await createServerDatabase();
      const child = spawn(serverBinary, ["--listen", "127.0.0.1:0"], {
        env: {
          ...process.env,
          DIFFX_SERVER_DATABASE_URL: database.url,
          DIFFX_SERVER_INSECURE_COOKIES: "true",
        },
        stdio: ["ignore", "pipe", "pipe"],
      });
      // "close" also follows a failed spawn, which never emits "exit".
      const exited = new Promise<void>((resolve) =>
        child.once("close", () => resolve()),
      );
      const stop = async () => {
        if (child.exitCode === null && child.signalCode === null) {
          child.kill("SIGTERM");
          await exited;
        }
        await database.drop();
      };
      try {
        child.stderr.on("data", (chunk) => process.stderr.write(chunk));
        // The server reports the bound listener once it serves.
        const address = await new Promise<string>((resolve, reject) => {
          const lines = createInterface({ input: child.stdout });
          const timer = setTimeout(
            () => reject(new Error("Timed out starting diffx-server")),
            60_000,
          );
          child.once("error", reject);
          child.once("exit", (code, signal) =>
            reject(
              new Error(
                `diffx-server exited before startup (${signal ?? code ?? "unknown"})`,
              ),
            ),
          );
          lines.on("line", (line) => {
            const listening = /listening on (\S+)/.exec(line)?.[1];
            if (!listening) return;
            clearTimeout(timer);
            resolve(listening);
          });
        });
        await use({ origin: `http://${address}` });
      } finally {
        await stop();
      }
    },
    { scope: "worker" },
  ],
  baseURL: async ({ remoteServer }, use) => use(remoteServer.origin),
});
