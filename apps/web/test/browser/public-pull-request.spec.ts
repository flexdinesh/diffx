import { type Page } from "@playwright/test";
import { createApiClient, type ApiPublicPullRequest } from "@diffx/api";
import { expect, reviewedToggle, test } from "./fixtures.ts";

const statusPath = "/api/v2/public/pull-requests/flexdinesh/diffx/157";

async function fixtureContext(baseURL: string | undefined) {
  if (!baseURL) throw new Error("Missing test URL");
  const catalog = await createApiClient({ baseUrl: baseURL }).GET(
    "/api/v2/contexts",
  );
  const contextId = catalog.data?.contexts[0]?.id;
  if (!contextId) throw new Error("Missing fixture context");
  return contextId;
}

function preview(
  contextId: string | null,
  status: ApiPublicPullRequest["status"],
): ApiPublicPullRequest {
  return {
    fullName: "flexdinesh/diffx",
    number: 157,
    status,
    contextId,
    fetchedAt: contextId ? Date.now() - 60_000 : null,
    failure: null,
    pullRequest: {
      provider: "github",
      repositoryId: 1,
      fullName: "flexdinesh/diffx",
      number: 157,
      url: "https://github.com/flexdinesh/diffx/pull/157",
      title: "fix: allow cross-site page navigations on server and console",
      body: "",
      state: "merged",
      draft: false,
      author: { login: "flexdinesh", url: "https://github.com/flexdinesh" },
      baseRef: "main",
      headRef: "fix/cross-site-navigation",
      headRepository: "flexdinesh/diffx",
      createdAt: 1_700_000_000_000,
      updatedAt: 1_700_000_100_000,
    },
  };
}

/**
 * A signed-out remote server with previews on. The preview fetches once, then
 * serves the worker's fixture snapshot through the public read routes, whose
 * server disables comments. Returns the paths the page requested.
 */
async function publicServer(page: Page, contextId: string) {
  const requested: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (path.startsWith("/api/")) requested.push(`${request.method()} ${path}`);
  });
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "remote",
        publicPullRequests: true,
        googleEnabled: false,
        signupEnabled: false,
        passwordSignupEnabled: false,
        user: null,
        csrfToken: null,
      },
    }),
  );
  let polls = 0;
  await page.route(`**${statusPath}`, (route) => {
    if (route.request().method() === "PUT")
      return route.fulfill({ json: preview(null, "fetching") });
    polls++;
    return route.fulfill({
      json: polls < 2 ? preview(null, "fetching") : preview(contextId, "ready"),
    });
  });
  await page.route("**/api/v2/public/contexts/**", async (route) => {
    const url = route
      .request()
      .url()
      .replace("/api/v2/public/contexts/", "/api/v2/contexts/");
    const response = await route.fetch({ url });
    const body: unknown = await response.json();
    // The public owner's comments capability is disabled server-side.
    const capabilities =
      typeof body === "object" && body !== null
        ? Reflect.get(body, "capabilities")
        : undefined;
    const review =
      typeof capabilities === "object" && capabilities !== null
        ? Reflect.get(capabilities, "review")
        : undefined;
    if (typeof review === "object" && review !== null)
      Reflect.set(review, "comments", { state: "disabled" });
    await route.fulfill({ response, json: body });
  });
  return requested;
}

async function pasteAndOpen(page: Page) {
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Review a pull request" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Open" }).click();
  await expect(
    page.getByText(/Enter a GitHub pull request link/),
  ).toBeVisible();
  await page
    .getByLabel("Pull request URL")
    .fill("github.com/flexdinesh/diffx/pull/157/files");
  await page.getByRole("button", { name: "Open" }).click();
  await expect(page).toHaveURL(/\/flexdinesh\/diffx\/pull\/157$/);
}

async function expectReadOnlyPreview(page: Page, requested: string[]) {
  await expect(
    page.getByText(/^Read-only preview of a public pull request/),
  ).toBeVisible();
  await expect(page.getByText("Badge.tsx").first()).toBeVisible();
  await expect(reviewedToggle(page)).toHaveCount(0);
  await expect(page.getByRole("button", { name: /Copy review/ })).toHaveCount(
    0,
  );
  // Previews read only public routes; account data is never requested.
  expect(
    requested.filter(
      (entry) =>
        entry.includes("/api/v2/contexts") ||
        entry.endsWith("/api/v2/events") ||
        entry.includes("/comments") ||
        entry.includes("/review-marks"),
    ),
  ).toEqual([]);
}

test("a visitor pastes a public pull request link and reads its preview", async ({
  page,
  baseURL,
}) => {
  const requested = await publicServer(page, await fixtureContext(baseURL));
  await pasteAndOpen(page);
  await expect(
    page.getByText(/Fetching flexdinesh\/diffx from GitHub/),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", {
      name: /#157 fix: allow cross-site page navigations/,
    }),
  ).toBeVisible();
  await expectReadOnlyPreview(page, requested);
  await expect(
    page.getByRole("banner").getByRole("link", { name: "Sign in" }),
  ).toHaveAttribute("href", "/signin");
});

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true });

  test("the preview keeps the compact review layout", async ({
    page,
    baseURL,
  }) => {
    const requested = await publicServer(page, await fixtureContext(baseURL));
    await pasteAndOpen(page);
    await expectReadOnlyPreview(page, requested);
  });
});
