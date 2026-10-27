import { type Page, type Route } from "@playwright/test";
import { expect, test } from "./fixtures.ts";
import { createApiClient, type ApiRepositoryDiff } from "@diffx/api";
import type { ReviewComment } from "../../src/review-comments.ts";

declare global {
  interface Window {
    reviewTestTools: {
      execute(name: string, input: Record<string, unknown>): Promise<unknown>;
    };
  }
}

async function fixture(baseURL: string | undefined) {
  if (!baseURL) throw new Error("Missing test URL");
  const client = createApiClient({ baseUrl: baseURL });
  const catalog = await client.GET("/api/v2/contexts");
  const contextId = catalog.data?.contexts[0]?.id;
  if (!contextId) throw new Error("Missing fixture context");
  const { data } = await client.GET(
    "/api/v2/contexts/{contextId}/diffs/current",
    { params: { path: { contextId }, query: { scope: "all" } } },
  );
  if (!data) throw new Error("Missing fixture diff");
  return data;
}

function commentFor(
  repository: ApiRepositoryDiff,
  body: string,
): ReviewComment {
  const file = repository.files.find(
    (file) => file.path === "src/components/Badge.tsx",
  );
  if (!file) throw new Error("Missing fixture file");
  return {
    id: "saved-comment",
    diffId: repository.id,
    versionId: repository.versionId,
    path: file.path,
    scope: "all",
    fingerprint: file.fingerprint,
    side: "additions",
    start: 3,
    end: 3,
    code: "+   return <span>{label}</span>;",
    body,
    status: "open",
    createdAt: 1,
    origin: {
      diffId: repository.id,
      versionId: repository.versionId,
      source: repository.source,
      repository: repository.name,
      branch: repository.branch,
      head: repository.head,
      revision: repository.revision,
      file: { status: file.status, oldPath: file.oldPath },
    },
  };
}

// The file sidebar sits beside the review column on desktop.
async function showDiff(page: Page) {
  await expect(page.locator("#sidebar")).toBeVisible();
}

async function openWorkspace(page: Page) {
  await page.route("**/api/v2/contexts/*/review-marks?*", (route) =>
    route.fulfill({ json: { marks: [] } }),
  );
  await page.setViewportSize({ width: 1280, height: 844 });
  await page.goto("/");
  await showDiff(page);
  await expect(page.locator("#additions")).toHaveText("+43");
  await page.evaluate(() => document.fonts.ready);
}

async function openDraft(page: Page, line = 3) {
  await page
    .locator('#file-tree [data-path="src/components/Badge.tsx"]')
    .click();
  const file = page
    .locator("#viewer diffs-container")
    .filter({ hasText: "src/components/Badge.tsx" });
  const input = page
    .locator("#viewer")
    .getByRole("textbox", { name: "Review comment" });
  await expect(async () => {
    if (await input.isVisible()) return;
    await file
      .locator(`[data-gutter] [data-column-number="${line}"]`)
      .hover({ timeout: 1_000 });
    await file
      .locator("[data-utility-button]")
      .click({ force: true, timeout: 1_000 });
    await expect(input).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 10_000 });
  return input;
}

function takeRequest(requests: Route[]) {
  const route = requests.shift();
  if (!route) throw new Error("Missing pending request");
  return route;
}

test("comment submission guards keyboard and button duplicates", async ({
  page,
  baseURL,
}) => {
  const repository = await fixture(baseURL);
  const requests: Route[] = [];
  let comments: ReviewComment[] = [];
  await page.route("**/api/v2/contexts/*/comments", (route) => {
    if (route.request().method() === "POST") {
      requests.push(route);
      return;
    }
    return route.fulfill({ json: { comments } });
  });
  await openWorkspace(page);
  const input = await openDraft(page);
  await input.fill("Submitted snapshot");
  await input.press("Control+Enter");
  await expect.poll(() => requests.length).toBe(1);
  await input.press("Control+Enter");
  const save = page.getByRole("button", { name: "Save comment" });
  await expect(save).toBeDisabled();
  expect(requests).toHaveLength(1);
  comments = [commentFor(repository, "Submitted snapshot")];
  await takeRequest(requests).fulfill({ status: 201, json: comments[0] });
  await expect(
    page.locator("#viewer").getByRole("textbox", { name: "Review comment" }),
  ).toHaveCount(0);
});

test("late save preserves newer text and retries it against the saved ID", async ({
  page,
  baseURL,
}) => {
  const repository = await fixture(baseURL);
  const requests: Route[] = [];
  let comments: ReviewComment[] = [];
  await page.route("**/api/v2/contexts/*/comments", (route) => {
    if (route.request().method() === "POST") {
      requests.push(route);
      return;
    }
    return route.fulfill({ json: { comments } });
  });
  await page.route("**/api/v2/contexts/*/comments/saved-comment", (route) => {
    requests.push(route);
  });
  await openWorkspace(page);
  const input = await openDraft(page);
  await input.fill("Submitted snapshot");
  await input.press("Control+Enter");
  await expect.poll(() => requests.length).toBe(1);
  await input.fill("Newer unsaved edit");
  comments = [commentFor(repository, "Submitted snapshot")];
  await takeRequest(requests).fulfill({ status: 201, json: comments[0] });
  await expect(input).toHaveValue("Newer unsaved edit");
  await expect(input).toBeFocused();
  await input.press("Control+Enter");
  await expect.poll(() => requests.length).toBe(1);
  const update = takeRequest(requests);
  expect(update.request().method()).toBe("PATCH");
  expect(update.request().postDataJSON()).toEqual({
    body: "Newer unsaved edit",
  });
  comments = [commentFor(repository, "Newer unsaved edit")];
  await update.fulfill({ json: comments[0] });
  await expect(
    page.locator("#viewer").getByRole("textbox", { name: "Review comment" }),
  ).toHaveCount(0);
});

test("WebMCP resolution shares comment ownership and preserves an active draft", async ({
  page,
  baseURL,
}) => {
  await page.addInitScript(() => {
    class ModelContextFake extends EventTarget implements WebMCP.ModelContext {
      ontoolchange:
        | ((this: WebMCP.ModelContext, event: Event) => unknown)
        | null = null;
      tools = new Map<string, WebMCP.ModelContextTool>();
      async registerTool(
        tool: WebMCP.ModelContextTool,
        options?: WebMCP.ModelContextRegisterToolOptions,
      ) {
        this.tools.set(tool.name, tool);
        options?.signal?.addEventListener("abort", () => {
          if (this.tools.get(tool.name) === tool) this.tools.delete(tool.name);
        });
      }
      async getTools() {
        return [];
      }
    }
    const modelContext = new ModelContextFake();
    Object.defineProperty(document, "modelContext", { value: modelContext });
    window.reviewTestTools = {
      async execute(name, input) {
        const tool = modelContext.tools.get(name);
        if (!tool) throw new Error("Tool not registered");
        return tool.execute(input, { signal: new AbortController().signal });
      },
    };
  });
  const repository = await fixture(baseURL);
  let comments = [commentFor(repository, "Resolve through WebMCP")];
  let fail = true;
  let holdReads = false;
  const requests: Route[] = [];
  await page.clock.install();
  await page.route("**/api/v2/contexts/*/comments", (route) => {
    if (holdReads) {
      requests.push(route);
      return;
    }
    return route.fulfill({ json: { comments } });
  });
  await page.route(
    "**/api/v2/contexts/*/review/comments/saved-comment/resolve",
    (route) => {
      if (fail)
        return route.fulfill({
          status: 500,
          json: { detail: "Resolve rejected" },
        });
      comments = comments.map((comment) => ({
        ...comment,
        status: "resolved",
      }));
      return route.fulfill({
        json: { comment_id: "saved-comment", status: "resolved" },
      });
    },
  );
  await openWorkspace(page);
  await expect(page.locator("#comment-count")).toHaveText("1");
  const input = await openDraft(page, 4);
  await input.fill("Keep this active draft");
  const failure = await page.evaluate(async () => {
    try {
      await window.reviewTestTools.execute("resolve_review_comment", {
        comment_id: "saved-comment",
      });
      return "Unexpected success";
    } catch (error) {
      return error instanceof Error ? error.message : "Unknown failure";
    }
  });
  expect(failure).toBe("Resolve rejected");
  await expect(page.locator("#comment-count")).toHaveText("1");
  const stale = [...comments];
  holdReads = true;
  await page.clock.fastForward(3_000);
  await expect.poll(() => requests.length).toBe(1);
  fail = false;
  await page.evaluate(() =>
    window.reviewTestTools.execute("resolve_review_comment", {
      comment_id: "saved-comment",
    }),
  );
  holdReads = false;
  await takeRequest(requests).fulfill({ json: { comments: stale } });
  await expect(input).toHaveValue("Keep this active draft");
  await input.press("Escape");
  await page.getByRole("tab", { name: /Review/ }).click();
  await page.getByRole("button", { name: "Filter comments: Open" }).click();
  await page.getByRole("menuitemradio", { name: "Resolved 1" }).click();
  await expect(
    page
      .locator('#comments-panel [data-comment-id="saved-comment"]')
      .getByTitle(/^Lifecycle:/),
  ).toHaveText("Resolved");
});
