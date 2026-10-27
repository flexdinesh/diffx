import { test as base, type Locator, type Page } from "@playwright/test";
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

type ToggleState = { pressed?: boolean; expanded?: boolean };

/** File header Reviewed toggles, named "Mark <path> reviewed|unreviewed". */
export function reviewedToggle(scope: Page | Locator, state?: ToggleState) {
  return scope.getByRole("button", {
    name: /^Mark .+ (?:un)?reviewed$/,
    ...state,
  });
}

/**
 * File header collapse toggles, named "Collapse|Expand <path>". Comment
 * toggles ("… comment at <path>") and the toolbar's "… all files" differ.
 */
export function fileToggle(scope: Page | Locator, state?: ToggleState) {
  return scope.getByRole("button", {
    name: /^(?:Collapse|Expand) (?!(?:\w+ )?comment at )(?!all files$)/,
    ...state,
  });
}

/**
 * The fixture server is a capture (one fixed patch), so the shell hides review
 * navigation. Serving a checkout-process bootstrap restores the header
 * comboboxes and search for tests that mock several contexts; the Reviews
 * column also needs two current reviews (see withSecondReview). Later
 * `page.route` registrations take precedence, so tests can still override it.
 */
export async function withReviewNavigation(
  page: Page,
  { collect = false }: { collect?: boolean } = {},
) {
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        googleEnabled: false,
        user: null,
        csrfToken: null,
        collect,
        input: "checkouts",
      },
    }),
  );
}

/**
 * A local process shows the Reviews column once it lists two current reviews.
 * Adds a second repository's copy of the fixture's review to every catalog
 * page, so layout tests over the single fixture review get the column.
 */
export async function withSecondReview(page: Page) {
  await page.route("**/api/v2/contexts?*", async (route) => {
    const response = await route.fetch();
    const body: unknown = await response.json();
    if (
      typeof body !== "object" ||
      body === null ||
      !("contexts" in body) ||
      !Array.isArray(body.contexts)
    )
      return route.fulfill({ response });
    const contexts: unknown[] = body.contexts;
    const first = contexts[0];
    if (typeof first !== "object" || first === null)
      return route.fulfill({ response });
    const second = {
      ...first,
      id: "second-review",
      streamId: "second-review",
      latestContextId: "second-review",
      name: "second-repo",
      repositoryId: "second-repo",
    };
    return route.fulfill({
      response,
      json: { ...body, contexts: [...contexts, second] },
    });
  });
}
