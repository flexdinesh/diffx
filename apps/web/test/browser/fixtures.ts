import { test as base } from "@playwright/test";
import { fileURLToPath } from "node:url";
import { preview } from "vite";
import { startFixtureProcess } from "../fixture-server.ts";

export { expect } from "@playwright/test";

type FixtureBuild = { binary: string | undefined; webDir: string | undefined };

/**
 * Each worker owns one in-memory fixture server behind a Vite preview of the
 * built app, which keeps SPA fallback for account routes.
 */
export const test = base.extend<
  object,
  { fixtureBuild: FixtureBuild; fixtureServer: string }
>({
  // Workers start after global setup, so its build paths are in the environment.
  fixtureBuild: [
    {
      binary: process.env.DIFFX_FIXTURE_BINARY,
      webDir: process.env.DIFFX_FIXTURE_WEB_DIR,
    },
    { scope: "worker", option: true },
  ],
  fixtureServer: [
    async ({ fixtureBuild: { binary, webDir } }, use) => {
      if (!binary || !webDir) throw new Error("Missing browser test build");
      const fixture = await startFixtureProcess({ binary });
      // vite.config.ts proxies the API to this worker's fixture.
      process.env.DIFFX_API_URL = fixture.target;
      const server = await preview({
        root: fileURLToPath(new URL("../..", import.meta.url)),
        logLevel: "warn",
        build: { outDir: webDir },
        preview: { host: "127.0.0.1", port: 0 },
      }).catch((error: unknown) => {
        fixture.stop();
        throw error;
      });
      const url = server.resolvedUrls?.local[0];
      try {
        if (!url) throw new Error("Missing browser test server URL");
        await use(url.replace(/\/$/, ""));
      } finally {
        await server.close();
        fixture.stop();
      }
    },
    { scope: "worker" },
  ],
  baseURL: async ({ fixtureServer }, use) => use(fixtureServer),
});
