import { type Page, type Route } from "@playwright/test";
import { expect, fileToggle, test } from "./fixtures.ts";
import type { ApiContext, ApiRepositoryDiff } from "@diffx/api";
import {
  comparisonKind,
  isDiffMode,
  type DiffMode,
} from "../../src/diff-mode.ts";
import type { ReviewComment } from "../../src/review-comments.ts";

async function openReviewPicker(page: Page) {
  await page.keyboard.press("ControlOrMeta+k");
}

async function localWorkspace(page: Page) {
  await page.route("**/api/v2/metrics", (route) =>
    route.fulfill({ json: { cpuUsage: 0, rssBytes: 0 } }),
  );
  const before = Array.from({ length: 100 }, (_, index) => `line ${index + 1}`);
  before[49] = 'export const value = "before";';
  const state = {
    version: 1,
    failPatch: false,
    emptyScopes: new Set<DiffMode>(),
    holdScopes: new Set<DiffMode>(),
    scopes: new Array<DiffMode>(),
    pendingScopes: new Array<{ route: Route; repository: ApiRepositoryDiff }>(),
    holdMarks: false,
    pendingMarks: new Array<Route>(),
    contentsRequests: 0,
    comments: new Array<ReviewComment>(),
  };
  const session: ApiContext = {
    id: "local-review",
    streamId: null,
    latestContextId: null,
    locationId: "location",
    repositoryId: "repo",
    kind: "observation",
    createdAt: 1,
    lastSubmittedAt: 1,
    lastChangedAt: 1,
    changedFileCount: 1,
    expiresAt: null,
    submittedFrom: null,
    availability: "available",
    stale: false,
    root: "/local-review",
    name: "Local review",
    branch: "main",
    worktreeName: null,
    capabilities: {
      diff: {
        scopes: { state: "enabled", values: ["all", "staged", "unstaged"] },
        refresh: { state: "enabled" },
        stagingMetadata: { state: "enabled" },
      },
      files: { contents: { state: "enabled" } },
      review: { comments: { state: "enabled" } },
    },
  };
  function repository(mode: DiffMode): ApiRepositoryDiff {
    return {
      id: `diff-${mode}`,
      versionId: `${mode}-v${state.version}`,
      locationId: "location",
      repositoryId: "repo",
      source: "local",
      root: session.root ?? "",
      name: session.name,
      branch: "main",
      head: "head",
      mode,
      comparisonKind: comparisonKind(session, mode),
      revision: `${mode}-v${state.version}`,
      files: state.emptyScopes.has(mode)
        ? []
        : [
            {
              id: "value-file",
              path: "src/value.ts",
              oldPath: null,
              status: "M",
              indexStatus: "M",
              worktreeStatus: "M",
              additions: 1,
              deletions: 1,
              binary: false,
              fingerprint: `${mode}-v${state.version}`,
            },
          ],
    };
  }
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: [session], nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) =>
    route.fulfill({ json: session }),
  );
  await page.route("**/api/v2/contexts/*/diffs/current?*", (route) => {
    const scope = new URL(route.request().url()).searchParams.get("scope");
    if (!isDiffMode(scope)) throw new Error("Invalid scope");
    state.scopes.push(scope);
    const diff = repository(scope);
    if (state.holdScopes.has(scope)) {
      state.pendingScopes.push({ route, repository: diff });
      return;
    }
    return route.fulfill({ json: diff });
  });
  await page.route("**/api/v2/contexts/*/diffs/*/files/*/patch?*", (route) => {
    if (state.failPatch)
      return route.fulfill({ status: 500, json: { detail: "Patch rejected" } });
    const version = new URL(route.request().url()).searchParams.get(
      "fileVersion",
    );
    return route.fulfill({
      json: {
        message: null,
        patch: [
          "diff --git a/src/value.ts b/src/value.ts",
          "--- a/src/value.ts",
          "+++ b/src/value.ts",
          "@@ -47,7 +47,7 @@",
          " line 47",
          " line 48",
          " line 49",
          '-export const value = "before";',
          `+export const value = "${version}";`,
          " line 51",
          " line 52",
          " line 53",
          "",
        ].join("\n"),
      },
    });
  });
  await page.route(
    "**/api/v2/contexts/*/diffs/*/files/*/contents?*",
    (route) => {
      state.contentsRequests++;
      const version = new URL(route.request().url()).searchParams.get(
        "fileVersion",
      );
      const after = [...before];
      after[49] = `export const value = "${version}";`;
      return route.fulfill({
        json: {
          before: before.join("\n") + "\n",
          after: after.join("\n") + "\n",
        },
      });
    },
  );
  await page.route("**/api/v2/contexts/*/comments", (route) =>
    route.fulfill({ json: { comments: state.comments } }),
  );
  await page.route("**/api/v2/contexts/*/review-marks?*", (route) =>
    route.fulfill({ json: { marks: [] } }),
  );
  await page.route("**/api/v2/contexts/*/review-marks/*?*", (route) => {
    if (state.holdMarks) {
      state.pendingMarks.push(route);
      return;
    }
    return route.fulfill({ json: {} });
  });
  return Object.assign(state, { context: session, repository });
}

async function ready(page: Page, value = "all-v1") {
  await expect(
    page.getByText(`export const value = "${value}";`, { exact: true }),
  ).toBeVisible();
}

/** The file sidebar sits beside the review column on desktop. */
async function showDiff(page: Page) {
  await expect(page.locator("#sidebar")).toBeVisible();
}

test("comparison uses the captured merge base and follows the selected scope", async ({
  page,
}) => {
  const state = await capturedWorkspace(page);
  const head = "a".repeat(40);
  const baseCommit = "b".repeat(40);
  const mergeBase = "c".repeat(40);
  state.context.kind = "observation";
  state.context.observation = {
    sourceId: "machine",
    hostname: "host",
    runId: "",
    agent: "",
    trigger: "manual",
    repositoryKey: "repo",
    repositoryName: "repo",
    remoteUrl: "",
    checkoutKey: "checkout",
    root: "/repo",
    worktreeName: "repo",
    branch: "feature",
    head,
    collectedAt: 1,
    collectorVersion: "test",
    comparison: {
      kind: "branch",
      baseRef: "refs/heads/main",
      baseCommit,
      mergeBase,
    },
  };
  await page.goto("/contexts/local-review?scope=all");
  await ready(page);
  const trigger = page.getByRole("button", { name: /^Diff comparison:/ });
  await expect(trigger).toContainText(
    "Merge base (main · ccccccc) → working tree",
  );
  await trigger.click();
  const popup = page.getByRole("dialog", { name: "Diff comparison" });
  await expect(popup).toContainText(
    "including committed, staged, unstaged, and untracked changes",
  );
  for (const value of ["refs/heads/main", head, baseCommit, mergeBase])
    await expect(popup.getByText(value, { exact: true })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(trigger).toBeFocused();
  await chooseComparison(page, "Uncommitted");
  await page.getByRole("button", { name: "Staged", exact: true }).click();
  await expect(trigger).toContainText("HEAD → index");
  await trigger.click();
  await expect(popup).toContainText("Committed branch changes are excluded");
  await expect(popup.getByText(head, { exact: true })).toBeVisible();
  await expect(popup.getByText(mergeBase, { exact: true })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Unstaged", exact: true }).click();
  await expect(trigger).toContainText("Index → working tree");
  await trigger.click();
  await expect(popup).toContainText("Unstaged changes compare the index");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "All changes", exact: true }).click();
  for (const width of [320, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await expect(page.locator(".topbar")).toHaveCSS("height", "44px");
    await expect(page.locator(".toolbar")).toHaveCSS("height", "89px");
    for (const name of ["All changes", "Staged", "Unstaged", "View options"]) {
      const control = page.getByRole("button", { name, exact: true });
      await expect(control).toBeVisible();
      const bounds = await control.boundingBox();
      if (!bounds) throw new Error(`Missing mobile control: ${name}`);
      expect(bounds.height).toBeGreaterThanOrEqual(44);
      expect(bounds.x).toBeGreaterThanOrEqual(0);
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
    }
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(trigger).toBeVisible();
  await expect(trigger).toContainText("Comparison");
  await chooseComparison(page, "Combined");
  await trigger.click();
  await expect(popup.getByText(mergeBase, { exact: true })).toBeVisible();
  const bounds = await popup.boundingBox();
  expect(bounds).not.toBeNull();
  if (bounds) {
    expect(bounds.x).toBeGreaterThanOrEqual(0);
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(390);
  }
});

test("working comparison identifies HEAD and unborn baselines", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.goto("/contexts/local-review");
  await ready(page);
  const trigger = page.getByRole("button", { name: /^Diff comparison:/ });
  await expect(trigger).toContainText("HEAD → working tree");
  await trigger.click();
  const popup = page.getByRole("dialog", { name: "Diff comparison" });
  await expect(popup).toContainText("Committed branch changes are excluded");
  await expect(popup.getByText("head", { exact: true })).toBeVisible();
  state.context.observation = {
    sourceId: "machine",
    hostname: "host",
    runId: "",
    agent: "",
    trigger: "manual",
    repositoryKey: "repo",
    repositoryName: "repo",
    remoteUrl: "",
    checkoutKey: "checkout",
    root: "/repo",
    worktreeName: "repo",
    branch: "main",
    head: null,
    collectedAt: 1,
    collectorVersion: "test",
    comparison: { kind: "working-tree", baseRef: "HEAD" },
  };
  await page.reload();
  await ready(page);
  await expect(trigger).toContainText("Empty tree → working tree");
  await trigger.click();
  await expect(popup).toContainText("Unborn (no commits)");
});

test("snapshot stays selected after newer captures and branch transitions", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  const original: ApiContext = {
    ...state.context,
    id: "original",
    kind: "observation",
    capabilities: {
      ...state.context.capabilities,
      diff: {
        ...state.context.capabilities.diff,
        refresh: { state: "unavailable" },
      },
    },
    observation: {
      sourceId: "machine",
      hostname: "host",
      runId: "",
      agent: "",
      trigger: "manual",
      repositoryKey: "repo",
      repositoryName: "repo",
      remoteUrl: "",
      checkoutKey: "checkout",
      root: "/repo",
      worktreeName: "repo",
      branch: "main",
      head: null,
      collectedAt: 1,
      collectorVersion: "test",
    },
  };
  const contexts: ApiContext[] = [original];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1);
    const context = contexts.find((entry) => entry.id === id);
    return route.fulfill({ json: context });
  });
  await page.goto("/contexts/original");
  await ready(page);
  await expect(page.locator("#connection")).toHaveText("Fixed snapshot");
  await expect(
    page.getByRole("button", {
      name: /^(Refresh|Retry loading) changes$/,
      includeHidden: true,
    }),
  ).toBeHidden();
  contexts.unshift({
    ...original,
    id: "clean-other-branch",
    branch: "feature",
    changedFileCount: 0,
  });
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await expect(page).toHaveURL(/\/contexts\/original$/);
  await ready(page);
  await page
    .getByRole("button", {
      name: "Leave review comment on file src/value.ts",
      exact: true,
    })
    .click();
  const draft = page.getByRole("textbox", { name: "Review comment" }).first();
  await draft.fill("Keep this unsaved review");
  await expect(page).toHaveURL(/\/contexts\/original$/);
  contexts.unshift({ ...original, id: "later-change" });
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await expect(draft).toHaveValue("Keep this unsaved review");
  await expect(page).toHaveURL(/\/contexts\/original$/);
  await page.goto("/contexts/clean-other-branch?watch=checkout&source=machine");
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await expect(page).toHaveURL(/\/contexts\/clean-other-branch\?watch=/);
});

test("rapid scopes ignore old diff responses and reviewed-file failures", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.goto("/");
  await ready(page);
  state.holdScopes.add("staged");
  await page.getByRole("button", { name: "Staged", exact: true }).click();
  await expect.poll(() => state.pendingScopes.length).toBe(1);
  await page.getByRole("button", { name: "Unstaged", exact: true }).click();
  await ready(page, "unstaged-v1");
  const stale = state.pendingScopes.shift();
  if (!stale) throw new Error("Missing staged request");
  await stale.route.fulfill({ json: stale.repository });
  await expect(
    page.getByRole("button", { name: "Unstaged", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await ready(page, "unstaged-v1");
  state.holdMarks = true;
  await page
    .getByRole("button", { name: "Mark src/value.ts reviewed" })
    .click();
  await expect.poll(() => state.pendingMarks.length).toBe(1);
  await page.getByRole("button", { name: "All changes", exact: true }).click();
  await ready(page);
  const mark = state.pendingMarks.shift();
  if (!mark) throw new Error("Missing mark request");
  await mark.fulfill({ status: 500, json: { detail: "Old scope failure" } });
  await expect(page.locator("#review-count")).toHaveText("0 of 1 reviewed");
  await expect(
    page.locator(".workspace-feedback").getByRole("alert"),
  ).toHaveCount(0);
  await expect(fileToggle(page.locator("#viewer"))).toHaveAttribute(
    "aria-expanded",
    "true",
  );
});

test("draft survives scopes and explicit reload without automatic diff polling", async ({
  page,
}) => {
  await page.clock.install();
  const state = await localWorkspace(page);
  await page.goto("/");
  await ready(page);
  await showDiff(page);
  const inline = page
    .locator("#viewer")
    .getByRole("textbox", { name: "Review comment" });
  // Worker rendering can replace the hover utility during initial highlighting.
  await expect(async () => {
    if (await inline.isVisible()) return;
    await page
      .locator('[data-gutter] [data-column-number="50"]')
      .last()
      .hover({ timeout: 1_000 });
    await page.locator("[data-utility-button]").click({ timeout: 1_000 });
    await expect(inline).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 10_000 });
  await inline.fill("Preserve this draft");
  const requestsBefore = state.scopes.length;
  await page.clock.fastForward(3_000);
  expect(state.scopes).toHaveLength(requestsBefore);
  await page.getByRole("button", { name: "Staged", exact: true }).click();
  await ready(page, "staged-v1");
  await page.getByRole("tab", { name: /Review/ }).click();
  const sidebar = page
    .locator("#comments-panel")
    .getByRole("textbox", { name: "Review comment" });
  await expect(sidebar).toHaveValue("Preserve this draft");
  await page.getByRole("button", { name: "All changes", exact: true }).click();
  await ready(page);
  await expect(inline).toHaveValue("Preserve this draft");
  state.version = 2;
  await page.getByRole("button", { name: "Refresh changes" }).click();
  await ready(page, "all-v2");
  await expect(sidebar).toHaveValue("Preserve this draft");
  await sidebar.press("Escape");
  const requestsAfter = state.scopes.length;
  await page.clock.fastForward(3_000);
  expect(state.scopes).toHaveLength(requestsAfter);
  await page.keyboard.press("Alt+/");
  await expect(
    page.getByRole("searchbox", { name: "Filter files" }),
  ).toBeFocused();
});

test("full-file context and selection survive layout changes", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.goto("/");
  await ready(page);
  await page.locator("[data-expand-button]").first().click();
  await expect.poll(() => state.contentsRequests).toBe(1);
  await expect(page.locator("#viewer")).toContainText("line 30");
  await page.getByRole("button", { name: "Unified", exact: true }).click();
  await expect(page.locator("#viewer")).toContainText("line 30");
  await ready(page);
  await page.getByRole("button", { name: "Split", exact: true }).click();
  const editor = page
    .locator("#viewer")
    .getByRole("textbox", { name: "Review comment" });
  // Worker rendering can replace the hover utility after switching layouts.
  await expect(async () => {
    if (await editor.isVisible()) return;
    await page
      .locator('[data-gutter] [data-column-number="50"]')
      .last()
      .hover({ timeout: 1_000 });
    await page.locator("[data-utility-button]").click({ timeout: 1_000 });
    await expect(editor).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 10_000 });
  await expect(
    page.locator("#viewer").getByText("src/value.ts:50", { exact: true }),
  ).toBeVisible();
  expect(state.contentsRequests).toBe(1);
});

test("failed preview retries without a revision change", async ({ page }) => {
  const state = await localWorkspace(page);
  state.failPatch = true;
  await page.goto("/");
  await expect(page.locator("#notice")).toContainText("Refresh to retry");
  await expect(page.locator("#viewer")).toContainText("Patch rejected");
  state.failPatch = false;
  await page.getByRole("button", { name: "Refresh changes" }).click();
  await ready(page);
  await expect(page.locator("#notice")).toBeHidden();
});

test("comment navigation changes scope and clears filters after previews load", async ({
  page,
}) => {
  const warnings: string[] = [];
  page.on("console", (message) => {
    if (message.text().includes("CodeView.scrollTo:"))
      warnings.push(message.text());
  });
  const state = await localWorkspace(page);
  state.comments = [
    {
      id: "staged-comment",
      diffId: "diff-staged",
      versionId: "staged-v1",
      path: "src/value.ts",
      scope: "staged",
      fingerprint: "staged-v1",
      side: "additions",
      start: 50,
      end: 50,
      code: '+export const value = "staged-v1";',
      body: "Navigate to staged change",
      status: "open",
      createdAt: 1,
    },
  ];
  await page.goto("/");
  await ready(page);
  await showDiff(page);
  await page.getByRole("searchbox", { name: "Filter files" }).fill("unmatched");
  await expect(page.locator("#viewer diffs-container")).toHaveCount(0);
  await page.getByRole("tab", { name: /Review/ }).click();
  await page
    .locator('#comments-panel [data-comment-id="staged-comment"]')
    .getByRole("button", { name: "src/value.ts:50", exact: true })
    .click();
  await expect(
    page.locator(
      "#viewer [data-additions] [data-line][data-comment-navigation-target]",
    ),
  ).toHaveCount(1);
  await ready(page, "staged-v1");
  await expect(
    page.getByRole("button", { name: "Staged", exact: true }),
  ).toHaveAttribute("aria-pressed", "true");
  await expect(
    page.locator('#viewer [data-comment-id="staged-comment"]'),
  ).toBeVisible();
  expect(warnings).toEqual([]);
  await page.getByRole("tab", { name: /Changes/ }).click();
  await expect(
    page.getByRole("searchbox", { name: "Filter files" }),
  ).toHaveValue("");
});

test("stream comments show moved and outdated placements and keep outdated comments open", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  const earlier: Pick<
    ReviewComment,
    | "diffId"
    | "versionId"
    | "path"
    | "scope"
    | "fingerprint"
    | "side"
    | "status"
  > = {
    diffId: "diff-earlier",
    versionId: "all-v0",
    path: "src/value.ts",
    scope: "all",
    fingerprint: "all-v0",
    side: "additions",
    status: "open",
  };
  const movedComment: ReviewComment = {
    ...earlier,
    id: "moved-comment",
    start: 40,
    end: 40,
    code: '+export const value = "all-v1";',
    body: "Moved concern",
    createdAt: 1,
    placement: {
      applicability: "moved",
      side: "additions",
      start: 50,
      end: 50,
    },
  };
  const outdatedComment: ReviewComment = {
    ...earlier,
    id: "outdated-comment",
    start: 10,
    end: 10,
    code: "+export const removed = true;",
    body: "Outdated concern",
    createdAt: 2,
    placement: {
      applicability: "outdated",
      side: "additions",
      start: 10,
      end: 10,
    },
  };
  state.comments = [movedComment, outdatedComment];
  const carried: string[] = [];
  await page.route("**/api/v2/contexts/*/comments/*/carry", (route: Route) => {
    carried.push(route.request().url());
    const comment: ReviewComment = {
      ...outdatedComment,
      diffId: "diff-all",
      versionId: "all-v1",
      fingerprint: "all-v1",
      target: "file",
      start: 0,
      end: 0,
      carriedFrom: {
        versionId: "all-v0",
        side: "additions",
        start: 10,
        end: 10,
      },
      placement: {
        applicability: "anchored",
        side: "additions",
        start: 0,
        end: 0,
      },
    };
    state.comments = [movedComment, comment];
    return route.fulfill({ json: comment });
  });
  await page.goto("/");
  await ready(page);

  const viewer = page.locator("#viewer");
  await expect(viewer.locator(".file-outdated-count")).toHaveText("1 outdated");
  await expect(
    page.locator("#file-panel .file-comment-count:not(.outdated)"),
  ).toHaveText("1");
  await expect(
    page.locator("#file-panel .file-comment-count.outdated"),
  ).toHaveText("1");
  await expect(
    viewer.locator('[data-comment-id="moved-comment"]'),
  ).toContainText("Moved from line 40");
  const outdated = viewer.locator('[data-comment-id="outdated-comment"]');
  await expect(outdated.getByText("Outdated", { exact: true })).toBeVisible();

  const notice = page
    .getByRole("status")
    .filter({ hasText: "1 open comment is outdated by code changes" });
  await expect(notice).toBeVisible();
  await notice.getByRole("button", { name: "Review outdated" }).click();
  const section = page.getByRole("region", { name: "Outdated comments" });
  await expect(section).toBeVisible();
  const card = section.locator(
    '[data-comment-id="outdated-comment"] [data-slot="card"]',
  );
  await expect(card).toHaveAttribute("data-expanded", "false");
  await card
    .getByRole("button", { name: /^Expand outdated comment at / })
    .click();
  await card.getByRole("button", { name: "Keep open", exact: true }).click();
  await expect.poll(() => carried.length).toBe(1);
  await expect(page.locator("#comment-feedback")).toContainText(
    "Comment kept open on its file.",
  );
  await expect(
    section.locator('[data-comment-id="outdated-comment"]'),
  ).toContainText("Outdated — kept open from line 10 of earlier code");
  await expect(
    section.getByRole("button", { name: "Keep open", exact: true }),
  ).toHaveCount(0);
});

test("context switching isolates delayed repository responses and fixed captures", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  const contexts: ApiContext[] = [
    state.context,
    {
      ...state.context,
      id: "second-repo",
      name: "Second repo",
      root: "/second-repo",
      repositoryId: "second-repo",
    },
    {
      ...state.context,
      id: "piped-patch",
      kind: "observation",
      source: "stdin",
      name: "Captured patch",
      root: null,
      locationId: null,
      repositoryId: null,
      capabilities: {
        ...state.context.capabilities,
        diff: {
          scopes: { state: "enabled", values: ["all"] },
          refresh: { state: "unavailable" },
          stagingMetadata: { state: "unavailable" },
        },
        files: { contents: { state: "unavailable" } },
      },
    },
  ];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1);
    const context = contexts.find((entry) => entry.id === id);
    return route.fulfill({ json: context });
  });
  let delayed: Route | undefined;
  let delay = false;
  await page.route("**/api/v2/contexts/*/diffs/current?*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/")[4];
    const context = contexts.find((entry) => entry.id === id);
    if (!context) throw new Error("Unknown context");
    if (delay && id === state.context.id) {
      delayed = route;
      return;
    }
    return route.fulfill({
      json: {
        ...state.repository("all"),
        id: `diff-${id}`,
        name: context.name,
        root: context.root ?? "",
        source: context.source === "stdin" ? "stdin" : "local",
      },
    });
  });
  await page.goto("/");
  await ready(page);
  delay = true;
  await page.getByRole("button", { name: "Refresh changes" }).click();
  await expect.poll(() => delayed !== undefined).toBe(true);
  await openReviewPicker(page);
  await page
    .getByRole("combobox", { name: "Search reviews" })
    .fill("Second repo");
  await page.keyboard.press("Enter");
  await page.keyboard.press("Enter");
  await expect(page.locator(".context-identity-name")).toHaveText(
    "Second repo",
  );
  await ready(page);
  if (!delayed) throw new Error("Missing delayed request");
  await delayed.fulfill({
    json: { ...state.repository("all"), name: "Stale repo" },
  });
  await expect(page.locator(".context-identity-name")).toHaveText(
    "Second repo",
  );
  await openReviewPicker(page);
  await page.getByRole("option", { name: /^Piped/ }).click();
  await page.getByRole("option").click();
  await expect(page.locator("#changes-title")).toHaveText("Piped");
  await expect(
    page.getByRole("button", { name: "Refresh changes" }),
  ).toBeHidden();
  await expect(page.locator("#connection")).toHaveText("Fixed snapshot");
  await expect(page).toHaveURL(/\/contexts\/piped-patch$/);
  await page.reload();
  await expect(page.locator("#changes-title")).toHaveText("Piped");
});

test("empty service explains local review and remote sync", async ({
  page,
}) => {
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: [], nextCursor: null } }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Run diffx to review a checkout, or diffx sync to publish remotely.",
    ),
  ).toBeVisible();
});

test("Piped history ignores repository filters and switches timestamp rows by keyboard", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  const piped: ApiContext = {
    ...state.context,
    id: "piped-observation",
    kind: "observation",
    source: "stdin",
    name: "Invoking project",
    branch: "invoking-branch",
    worktreeName: "invoking-worktree",
    stale: true,
    createdAt: new Date(2026, 9, 5, 20, 17).getTime(),
  };
  const newer: ApiContext = {
    ...piped,
    id: "piped-empty",
    createdAt: new Date(2026, 9, 5, 20, 18).getTime(),
    changedFileCount: 0,
  };
  const contexts = [state.context, piped, newer];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1);
    return route.fulfill({
      json: contexts.find((context) => context.id === id),
    });
  });
  await page.route("**/api/v2/contexts/*/diffs/current?*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/")[4];
    return route.fulfill({
      json: {
        ...state.repository("all"),
        source: id === state.context.id ? "local" : "stdin",
      },
    });
  });
  await page.goto("/");
  await ready(page);
  await openReviewPicker(page);
  const dialog = page.getByRole("dialog", { name: "Switch review" });
  await dialog.getByRole("button", { name: "Filters", exact: true }).click();
  await dialog.getByRole("button", { name: "Stale", exact: true }).click();
  await expect(dialog.getByRole("option")).toHaveCount(1);
  await expect(dialog.getByRole("option")).toContainText("Piped");
  await dialog.getByRole("combobox").fill("piped");
  await page.keyboard.press("Enter");
  await expect(
    dialog.getByRole("combobox", { name: "Search Piped" }),
  ).toHaveValue("");
  await expect(
    dialog.getByRole("button", { name: "Filters", exact: true }),
  ).toHaveCount(0);
  const rows = dialog.getByRole("option");
  await expect(rows).toHaveCount(2);
  await expect(rows.first()).toContainText("No changes");
  await expect(dialog).not.toContainText("Invoking project");
  await expect(dialog).not.toContainText("invoking-branch");
  await expect(rows).not.toContainText(["Stale", "Stale"]);
  await expect(
    dialog.getByRole("button", { name: "Stale", exact: true }),
  ).toBeHidden();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/contexts\/piped-observation$/);
  const identity = page.locator(".context-identity");
  await expect(identity).toContainText("Piped");
  await expect(identity).not.toContainText("Invoking project");
  await expect(identity.locator(".context-identity-worktree")).toHaveCount(0);
  await expect(page).toHaveTitle("diffx · Piped");
  await openReviewPicker(page);
  await page.keyboard.press("Enter");
  await expect(
    dialog.getByRole("option", { name: /Current review/ }),
  ).toHaveCount(1);
  await page.keyboard.press("ArrowLeft");
  await expect(
    dialog.getByRole("combobox", { name: "Search reviews" }),
  ).toBeVisible();
  await expect(
    dialog.getByText("Stale snapshots · With changes", { exact: true }),
  ).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeHidden();
});

for (const kind of ["observation"] satisfies ApiContext["kind"][]) {
  test(`landing stays unselected when ${kind} contexts have no usable diff`, async ({
    page,
  }) => {
    const state = await localWorkspace(page);
    const contexts: ApiContext[] = [
      { ...state.context, kind, changedFileCount: 0 },
      { ...state.context, kind, id: "unknown", changedFileCount: null },
      {
        ...state.context,
        kind,
        id: "unavailable",
        availability: "unavailable",
      },
    ];
    await page.route("**/api/v2/contexts?*", (route) =>
      route.fulfill({ json: { contexts, nextCursor: null } }),
    );
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: "No changes to review" }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/$/);
    await expect(page.locator(".context-identity")).toHaveCount(0);
    expect(state.scopes).toEqual([]);
    await openReviewPicker(page);
    const dialog = page.getByRole("dialog", { name: "Switch review" });
    if (state.context.source === "stdin") {
      await expect(dialog.getByRole("option")).toHaveCount(1);
      await dialog.getByRole("option", { name: /^Piped/ }).click();
      await expect(
        dialog.getByRole("option", { name: /Unavailable/ }),
      ).toBeDisabled();
    } else {
      await expect(dialog.getByRole("option")).toHaveCount(0);
      await dialog
        .getByRole("button", { name: "Filters", exact: true })
        .click();
      await dialog.getByRole("checkbox", { name: "All", exact: true }).check();
    }
    await expect(dialog.getByRole("option")).toHaveCount(
      kind === "observation" ? 1 : 3,
    );
    await page.keyboard.press("Escape");
    await page.setViewportSize({ width: 360, height: 640 });
    await expect(
      page.getByRole("heading", { name: "No changes to review" }),
    ).toBeVisible();
    await page.reload();
    await expect(
      page.getByRole("heading", { name: "No changes to review" }),
    ).toBeVisible();
    await expect(page).toHaveURL(/\/$/);
  });
}

test("landing selects the most active usable diff across catalog pages", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  state.context.lastChangedAt = 30;
  await page.route("**/api/v2/contexts?*", (route) => {
    const cursor = new URL(route.request().url()).searchParams.get("cursor");
    return route.fulfill({
      json: cursor
        ? {
            contexts: [
              {
                ...state.context,
                id: "later",
                lastChangedAt: 20,
                lastSubmittedAt: 900,
              },
              state.context,
            ],
            nextCursor: null,
          }
        : {
            contexts: [
              { ...state.context, id: "empty", changedFileCount: 0 },
              { ...state.context, id: "unknown", changedFileCount: null },
              {
                ...state.context,
                id: "unavailable",
                availability: "unavailable",
              },
            ],
            nextCursor: "next",
          },
    });
  });
  await page.goto("/");
  await ready(page);
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
});

test("landing selects a diff when a catalog refresh adds changes", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  state.context.changedFileCount = 0;
  await page.route("**/api/v2/events", (route) => route.abort());
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "No changes to review" }),
  ).toBeVisible();
  state.context.changedFileCount = 1;
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await ready(page);
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
});

test("empty contexts remain accessible through the picker and direct URLs", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  state.context.changedFileCount = 0;
  await page.route("**/api/v2/contexts/*/diffs/current?*", (route) =>
    route.fulfill({ json: { ...state.repository("all"), files: [] } }),
  );
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "No changes to review" }),
  ).toBeVisible();
  await openReviewPicker(page);
  const dialog = page.getByRole("dialog", { name: "Switch review" });
  await dialog.getByRole("button", { name: "Filters", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "All", exact: true }).check();
  await dialog.getByRole("option", { name: /Local review/ }).click();
  await dialog.getByRole("option").click();
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
  await expect(
    page.getByRole("heading", { name: "No net uncommitted changes" }),
  ).toBeVisible();
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "No net uncommitted changes" }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
});

test("unavailable context keeps the switcher usable", async ({ page }) => {
  const state = await localWorkspace(page);
  const unavailable: ApiContext = {
    ...state.context,
    id: "unavailable",
    name: "Unavailable repo",
    availability: "unavailable",
  };
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({
      json: { contexts: [state.context, unavailable], nextCursor: null },
    }),
  );
  await page.route("**/api/v2/contexts/unavailable", (route) =>
    route.fulfill({ status: 503, json: { detail: "Repository moved" } }),
  );
  await page.goto("/contexts/unavailable");
  await expect(page.getByText("Repository moved")).toBeVisible();
  await openReviewPicker(page);
  await page
    .getByRole("combobox", { name: "Search reviews" })
    .fill("Local review");
  await page.keyboard.press("Enter");
  await page.keyboard.press("Enter");
  await ready(page);
  await openReviewPicker(page);
  const dialog = page.getByRole("dialog", { name: "Switch review" });
  await dialog.getByRole("button", { name: "Filters", exact: true }).click();
  await dialog.getByRole("checkbox", { name: "All", exact: true }).check();
  const search = dialog.getByRole("combobox", { name: "Search reviews" });
  await search.fill("Unavailable repo");
  await expect(dialog.getByRole("option")).toBeDisabled();
  await search.press("Enter");
  await expect(dialog).toBeVisible();
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
});

test("observation picker drills into repositories and searches independent sources on the same branch", async ({
  page,
}) => {
  await page.addInitScript(() => {
    class IngestionEvents extends EventTarget {
      notify = () =>
        this.dispatchEvent(new MessageEvent("ingestion", { data: "{}" }));
      constructor() {
        super();
        window.addEventListener("test-ingestion", this.notify);
      }
      close() {
        window.removeEventListener("test-ingestion", this.notify);
      }
    }
    Object.defineProperty(window, "EventSource", { value: IngestionEvents });
  });
  const state = await localWorkspace(page);
  function observation(
    id: string,
    hostname: string,
    runId: string,
    collectedAt: number,
  ): ApiContext {
    return {
      ...state.context,
      id,
      kind: "observation",
      worktreeName: "main-checkout",
      lastChangedAt: collectedAt,
      observation: {
        sourceId: `source-${hostname}`,
        hostname,
        runId,
        agent: "test-agent",
        trigger: "hook",
        repositoryKey: "repo",
        repositoryName: "Local review",
        remoteUrl: "https://example.test/review.git",
        checkoutKey: "checkout",
        root: "/workspace/review",
        worktreeName: "main-checkout",
        ...(id === "second" ? { linkedWorktree: true } : {}),
        branch: "main",
        head: "head",
        collectedAt,
        collectorVersion: "test",
      },
      capabilities: {
        ...state.context.capabilities,
        diff: {
          ...state.context.capabilities.diff,
          refresh: { state: "unavailable" },
        },
      },
    };
  }
  const contexts = [
    { ...observation("first", "container-a", "run-a", 1000), stale: true },
    observation("second", "container-b", "run-b", 2000),
    observation("third", "container-a", "run-a", 3000),
  ];
  let catalogRequests = 0;
  await page.route("**/api/v2/contexts?*", (route) => {
    catalogRequests++;
    return route.fulfill({ json: { contexts, nextCursor: null } });
  });
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1);
    return route.fulfill({
      json: contexts.find((context) => context.id === id),
    });
  });
  await page.route("**/api/v2/events", (route) => route.abort());
  await page.clock.install();
  await page.goto("/");
  await ready(page);
  await expect(
    page.getByRole("button", { name: "Refresh changes" }),
  ).toBeHidden();
  await expect(page.locator("#changes-title")).toHaveClass("sr-only");
  await expect(page.locator("#repo-path")).toContainText("container-a");
  await expect(page.locator("#repo-path")).not.toContainText("run-a");
  await expect(page.locator("#repo-path")).not.toContainText(
    "source-container-a",
  );
  await expect(page.locator("#repo-path")).not.toContainText("Collected");
  await expect(page.locator("#repo-path")).toHaveAttribute(
    "title",
    /Context: third/,
  );
  await expect(page).toHaveURL(/\/contexts\/third$/);
  await expect(page.locator(".context-identity-worktree")).toHaveCount(0);
  const requests = state.scopes.length;
  const catalogReads = catalogRequests;
  await page.clock.fastForward(9000);
  expect(state.scopes).toHaveLength(requests);
  expect(catalogRequests).toBe(catalogReads);
  await openReviewPicker(page);
  const dialog = page.getByRole("dialog", { name: "Switch review" });
  const search = dialog.getByRole("combobox", { name: "Search reviews" });
  await expect(dialog.getByRole("option")).toHaveCount(1);
  await expect(dialog.getByRole("option")).toContainText("2 snapshots");
  await search.press("Enter");
  await expect(
    dialog.getByRole("listbox", { name: "Collected snapshots" }),
  ).toBeVisible();
  await expect(dialog.getByRole("option")).toHaveCount(2);
  await expect(dialog.getByRole("option", { name: /container-a/ })).toHaveCount(
    1,
  );
  await search.fill("main container-b run-b");
  await expect(dialog.getByRole("option")).toHaveCount(1);
  await dialog.getByRole("button", { name: "Back to reviews" }).click();
  await expect(search).toBeFocused();
  await expect(search).toHaveValue("main container-b run-b");
  await expect(dialog.getByRole("option")).toContainText("1 snapshot");
  await search.press("Enter");
  await search.press("Enter");
  await expect(page).toHaveURL(/\/contexts\/second$/);
  // Choosing a review moves focus to its stream in the review column.
  await expect(
    page.locator('#projects-nav [aria-current="page"]'),
  ).toBeFocused();
  await expect(page.locator(".context-identity-worktree")).toHaveAccessibleName(
    "Linked worktree: main-checkout",
  );
  await expect(page.locator("#repo-path")).toContainText("container-b");
  await expect(page.locator("#repo-path")).not.toContainText("run-b");
  await expect(page.locator(".context-identity")).toHaveAttribute(
    "title",
    /Run: run-b[\s\S]*Context: second/,
  );
  await page.setViewportSize({ width: 360, height: 640 });
  await expect(page.locator(".context-identity-worktree")).toBeVisible();
  await expect(page.locator(".context-identity-worktree-name")).toBeHidden();
  await openReviewPicker(page);
  await search.press("Enter");
  const bounds = await dialog.boundingBox();
  if (!bounds) throw new Error("Missing picker bounds");
  expect(bounds.x).toBeGreaterThanOrEqual(0);
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(360);
  await search.fill("not-a-source");
  await expect(
    dialog.getByText("No snapshots found", { exact: true }),
  ).toBeVisible();
  await search.fill("");
  await search.press("ArrowLeft");
  await expect(dialog.getByRole("listbox", { name: "Reviews" })).toBeVisible();
  const selected = contexts.find((context) => context.id === "second");
  if (!selected) throw new Error("Missing selected observation");
  selected.stale = true;
  contexts.push(observation("fourth", "container-b", "run-new", 4000));
  const diffReads = state.scopes.length;
  await page.evaluate(() => window.dispatchEvent(new Event("test-ingestion")));
  await expect(dialog.getByRole("option")).toContainText("2 snapshots");
  await expect(page).toHaveURL(/\/contexts\/second$/);
  expect(state.scopes).toHaveLength(diffReads);
  await dialog.getByRole("button", { name: "Filters", exact: true }).click();
  await dialog.getByRole("button", { name: "All", exact: true }).click();
  await expect(dialog.getByRole("option")).toContainText("4 snapshots");
  await search.press("Enter");
  await expect(dialog.getByRole("option")).toHaveCount(4);
  await expect(
    dialog.getByRole("option", { name: /container-b.*run-b/ }),
  ).toContainText("Stale");
  await expect(page).toHaveURL(/\/contexts\/second$/);
});

test("picker combines filters before grouping and preserves them across navigation and reopening", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  function snapshot(
    id: string,
    hostname: string,
    branch: string,
    worktreeName: string,
    changedFileCount: number,
  ): ApiContext {
    return {
      ...state.context,
      id,
      kind: "observation",
      branch,
      worktreeName,
      changedFileCount,
      observation: {
        sourceId: `source-${hostname}`,
        hostname,
        runId: "run",
        agent: "",
        trigger: "review",
        repositoryKey: "repo",
        repositoryName: state.context.name,
        remoteUrl: "https://example.test/review.git",
        checkoutKey: worktreeName,
        root: `/workspace/${worktreeName}`,
        worktreeName,
        branch,
        head: "head",
        collectedAt: 1000,
        collectorVersion: "test",
      },
    };
  }
  const contexts = [
    snapshot("first", "host-a", "main", "tree-a", 1),
    snapshot("second", "host-b", "feature", "tree-b", 2),
    snapshot("third", "host-c", "main", "tree-c", 0),
    {
      ...state.context,
      id: "missing",
      name: "Missing checkout",
      repositoryId: "missing-repository",
      availability: "unavailable",
    },
  ];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1);
    return route.fulfill({
      json: contexts.find((context) => context.id === id),
    });
  });
  await page.goto("/");
  await ready(page);
  await openReviewPicker(page);
  const dialog = page.getByRole("dialog", { name: "Switch review" });
  const search = dialog.getByRole("combobox", { name: "Search reviews" });
  await expect(dialog.getByRole("option")).toContainText("2 snapshots");
  await expect(
    dialog.getByRole("option", { name: /Missing checkout/ }),
  ).toHaveCount(0);
  await dialog.getByRole("button", { name: "Filters", exact: true }).click();
  await dialog.getByRole("button", { name: "Host", exact: true }).click();
  await page
    .getByRole("menuitemcheckbox", { name: "host-a", exact: true })
    .click();
  await page
    .getByRole("menuitemcheckbox", { name: "host-b", exact: true })
    .click();
  await page.keyboard.press("Escape");
  await expect(dialog.getByRole("option")).toContainText("2 snapshots");
  await dialog.getByRole("button", { name: "Branch", exact: true }).click();
  await page
    .getByRole("menuitemcheckbox", { name: "feature", exact: true })
    .click();
  await page.keyboard.press("Escape");
  await expect(dialog.getByRole("option")).toContainText("1 snapshot");
  await dialog.getByRole("button", { name: "Worktree", exact: true }).click();
  await page
    .getByRole("menuitemcheckbox", { name: "tree-b", exact: true })
    .click();
  await page.keyboard.press("Escape");
  await dialog.getByRole("button", { name: "Stale", exact: true }).click();
  await expect(
    dialog.getByText("No reviews found", { exact: true }),
  ).toBeVisible();
  await dialog.getByRole("button", { name: "Latest", exact: true }).click();
  await search.focus();
  await search.press("Enter");
  await expect(dialog.getByRole("option")).toHaveCount(1);
  await expect(dialog.getByRole("option")).toContainText("tree-b");
  await dialog.getByRole("button", { name: "Back to reviews" }).click();
  await expect(dialog.getByRole("option")).toContainText("1 snapshot");
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await openReviewPicker(page);
  await expect(dialog.getByRole("option")).toContainText("1 snapshot");
  await expect(
    dialog.getByText(
      "Latest snapshots · With changes · Host: host-a · Host: host-b · Branch: feature · Worktree: tree-b",
      { exact: true },
    ),
  ).toBeVisible();
  await page.setViewportSize({ width: 360, height: 640 });
  const bounds = await dialog.boundingBox();
  if (!bounds) throw new Error("Missing picker bounds");
  expect(bounds.x).toBeGreaterThanOrEqual(0);
  expect(bounds.x + bounds.width).toBeLessThanOrEqual(360);
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(640);
  await dialog
    .getByRole("button", { name: "Clear filters", exact: true })
    .click();
  await expect(dialog.getByRole("option")).toContainText("2 snapshots");
  await dialog.getByRole("checkbox", { name: "All", exact: true }).check();
  await expect(dialog.getByRole("option", { name: /3 snapshots/ })).toHaveCount(
    1,
  );
  const unavailable = dialog.getByRole("option", { name: /Missing checkout/ });
  await expect(unavailable).toBeDisabled();
  await expect(unavailable).toContainText("Snapshot unavailable");
  await search.fill("Missing checkout");
  await search.press("ArrowDown");
  await search.press("Enter");
  await expect(dialog).toBeVisible();
  await expect(search).not.toHaveAttribute("aria-activedescendant");
  await expect(page).toHaveURL(/\/contexts\/first$/);
  await search.fill("");
  await search.press("ArrowDown");
  await expect(unavailable).toHaveAttribute("aria-selected", "false");
});

test("repos drawer lists current changed streams and announces newer snapshots", async ({
  page,
}) => {
  await page.addInitScript(() => {
    class IngestionEvents extends EventTarget {
      notify = () =>
        this.dispatchEvent(new MessageEvent("ingestion", { data: "{}" }));
      constructor() {
        super();
        window.addEventListener("test-ingestion", this.notify);
      }
      close() {
        window.removeEventListener("test-ingestion", this.notify);
      }
    }
    Object.defineProperty(window, "EventSource", { value: IngestionEvents });
  });
  const state = await localWorkspace(page);
  const stream = (
    id: string,
    name: string,
    branch: string,
    lastSubmittedAt: number,
    changedFileCount = 1,
  ): ApiContext => ({
    ...state.context,
    id,
    name,
    branch,
    repositoryId: name,
    streamId: `stream-${branch}`,
    latestContextId: id,
    lastSubmittedAt,
    lastChangedAt: lastSubmittedAt,
    changedFileCount,
  });
  const viewed = stream("rate-1", "acme-api", "feat/rate-limit", 10);
  const contexts = new Map(
    [
      viewed,
      stream("web-1", "web-app", "main", 20),
      stream("infra-1", "infra", "cleanup", 30, 0),
    ].map((context) => [context.id, context]),
  );
  let heads = [...contexts.values()];
  const latestRequests: string[] = [];
  await page.route("**/api/v2/contexts?*", (route) => {
    latestRequests.push(
      new URL(route.request().url()).searchParams.get("latest") ?? "",
    );
    return route.fulfill({ json: { contexts: heads, nextCursor: null } });
  });
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    return route.fulfill({ json: contexts.get(id) });
  });
  await page.goto("/contexts/rate-1");
  await ready(page);
  expect(latestRequests).toContain("true");
  const nav = page.locator("#projects-nav");
  // The review column and the open review's file sidebar show together.
  await expect(nav).toBeVisible();
  await expect(nav).toHaveAccessibleName("Reviews");
  await expect(page.locator("#sidebar")).toBeVisible();
  await expect(
    nav.getByRole("button", { name: /^(web-app|acme-api)/ }),
  ).toHaveText([/web-app/, /acme-api/]);
  await expect(nav).not.toContainText("infra");
  await expect(
    nav.getByRole("link", { name: /feat\/rate-limit/ }),
  ).toHaveAttribute("aria-current", "page");
  await expect(
    nav.getByRole("link", { name: /feat\/rate-limit/ }),
  ).toHaveAttribute("href", "/streams/stream-feat%2Frate-limit");
  await expect(
    page.getByRole("status").filter({ hasText: "Newer snapshot" }),
  ).toBeHidden();

  const newer = {
    ...stream("rate-2", "acme-api", "feat/rate-limit", 40),
    changedFileCount: 3,
  };
  contexts.set(newer.id, newer);
  heads = [newer, ...heads.filter((context) => context.id !== viewed.id)];
  await page.evaluate(() => window.dispatchEvent(new Event("test-ingestion")));
  const notice = page.getByRole("status").filter({ hasText: "Newer snapshot" });
  await expect(notice).toContainText("Newer snapshot of feat/rate-limit");
  await expect(notice).toContainText("3 changed files");
  await expect(page).toHaveURL(/\/contexts\/rate-1$/);
  await notice.getByRole("button", { name: "View latest" }).click();
  await expect(page).toHaveURL(/\/streams\/stream-feat%2Frate-limit$/);
  await expect(notice).toBeHidden();

  // Choosing a stream replaces the review in place; both panes stay open.
  const mainStream = nav.getByRole("link", { name: /^main/ });
  await mainStream.click();
  await expect(page).toHaveURL(/\/streams\/stream-main$/);
  await expect(nav).toBeVisible();
  await expect(page.locator("#sidebar")).toBeVisible();
  await expect(mainStream).toHaveAttribute("aria-current", "page");
  await expect(mainStream).toBeFocused();
  await expect(page.locator(".context-identity")).toContainText("web-app");
  await expect(nav.getByRole("searchbox")).toHaveCount(0);
  await nav.getByRole("button", { name: "Search reviews" }).click();
  const picker = page.getByRole("dialog", { name: "Switch review" });
  await expect(picker).toBeVisible();
  await picker
    .getByRole("combobox", { name: "Search reviews" })
    .fill("acme rate-limit");
  await expect(picker.getByRole("option")).toHaveCount(1);
  await expect(picker.getByRole("option")).toContainText("acme-api");
  await page.keyboard.press("Escape");
  await expect(picker).toBeHidden();
  await expect(
    nav.getByRole("button", { name: "Search reviews" }),
  ).toBeFocused();

  // Recent first interleaves repositories by activity, labeling each row.
  await nav.getByRole("button", { name: "Review list view" }).click();
  await page.getByRole("menuitemradio", { name: "Recent first" }).click();
  await page.keyboard.press("Escape");
  await expect(nav.getByRole("link")).toHaveText([
    /feat\/rate-limit[\s\S]*acme-api/,
    /main[\s\S]*web-app/,
  ]);

  // The rail marks each repository and highlights the open review's.
  await nav.getByRole("button", { name: "Collapse reviews" }).click();
  const marks = nav
    .getByRole("list", { name: "Repositories" })
    .getByRole("button");
  await expect(marks).toHaveText(["AA", "WA"]);
  await expect(marks.nth(1)).toHaveAttribute("aria-current", "true");
  await expect(marks.nth(0)).not.toHaveAttribute("aria-current");
  await page.reload();
  await ready(page);
  await expect(marks).toHaveCount(2);
  // Expanding from a mark returns to the list at that repository.
  await nav.getByRole("button", { name: "acme-api" }).click();
  await expect(
    nav.getByRole("link", { name: /^feat\/rate-limit/ }),
  ).toBeFocused();
  await expect(nav.getByRole("link")).toHaveText([
    /feat\/rate-limit[\s\S]*acme-api/,
    /main[\s\S]*web-app/,
  ]);

  await page.setViewportSize({ width: 390, height: 844 });
  const mobileTrigger = page.getByRole("button", { name: "Show reviews" });
  await mobileTrigger.click();
  await expect(nav).toBeVisible();
  await expect(nav).toHaveAttribute("role", "dialog");
  await expect(nav).toHaveAttribute("aria-modal", "true");
  await expect(page.locator("main")).toHaveAttribute("inert", "");
  await expect(page.locator("#sidebar")).toBeHidden();
  await page.keyboard.press("Escape");
  await expect(nav).toBeHidden();
  await expect(mobileTrigger).toBeFocused();
  await mobileTrigger.click();
  // Choosing a stream in the drawer closes it and shows the diff. The drawer
  // focuses its first action, whose tooltip covers the first row.
  await nav.getByRole("link", { name: /^feat\/rate-limit/ }).focus();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/streams\/stream-feat%2Frate-limit$/);
  await expect(nav).toBeHidden();
  await expect(page.locator("main")).not.toHaveAttribute("inert", "");
  await expect(page.locator(".context-identity")).toContainText("acme-api");
  await expect(
    page.getByRole("button", { name: "Show file sidebar" }),
  ).toBeFocused();
});

test("header switcher and inbox open streams over a preserved review", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  const stream = (
    id: string,
    name: string,
    branch: string,
    lastSubmittedAt: number,
  ): ApiContext => ({
    ...state.context,
    id,
    name,
    branch,
    repositoryId: name,
    streamId: `stream-${branch}`,
    latestContextId: id,
    lastSubmittedAt,
    lastChangedAt: lastSubmittedAt,
  });
  const contexts = new Map(
    [
      stream("rate-1", "acme-api", "feat/rate-limit", 10),
      stream("auth-1", "acme-api", "fix/auth", 25),
      stream("web-1", "web-app", "main", 20),
    ].map((context) => [context.id, context]),
  );
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({
      json: { contexts: [...contexts.values()], nextCursor: null },
    }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    return route.fulfill({ json: contexts.get(id) });
  });
  await page.setViewportSize({ width: 1280, height: 480 });
  await page.goto("/contexts/rate-1");
  await ready(page);
  const nav = page.locator("#projects-nav");
  const identity = page.locator(".context-identity");

  // The header repository menu opens that repository's latest stream.
  await page
    .getByRole("button", { name: "Repository acme-api. Switch repository" })
    .click();
  await page.getByRole("menuitemradio", { name: /web-app/ }).click();
  await expect(page).toHaveURL(/\/streams\/stream-main$/);
  await expect(identity).toContainText("web-app");
  await expect(nav.getByRole("link", { name: /^main/ })).toBeFocused();

  // The branch menu lists only the open repository's streams.
  await page
    .getByRole("button", { name: "Repository web-app. Switch repository" })
    .click();
  await page.getByRole("menuitemradio", { name: /acme-api/ }).click();
  await expect(page).toHaveURL(/\/streams\/stream-fix%2Fauth$/);
  await page
    .getByRole("button", { name: "Branch fix/auth. Switch branch" })
    .click();
  const branches = page.getByRole("menuitemradio");
  await expect(branches).toHaveText([/fix\/auth/, /feat\/rate-limit/]);
  await branches.filter({ hasText: "feat/rate-limit" }).click();
  await expect(page).toHaveURL(/\/streams\/stream-feat%2Frate-limit$/);
  await expect(identity).toContainText("feat/rate-limit");
  await ready(page);

  // The inbox covers the review without resizing or scrolling it.
  const scroller = page.locator(".diff-code-view");
  await scroller.evaluate((element) => {
    element.scrollTop = 200;
  });
  const top = await scroller.evaluate((element) => element.scrollTop);
  expect(top).toBeGreaterThan(0);
  const opener = nav.getByRole("button", { name: "Open inbox" });
  await opener.click();
  const inbox = page.getByRole("region", { name: "Inbox" });
  await expect(inbox).toBeVisible();
  await expect(
    inbox.getByRole("searchbox", { name: "Filter reviews" }),
  ).toBeFocused();
  await expect(page.locator("main")).toHaveAttribute("inert", "");
  await expect(inbox.getByRole("link")).toHaveCount(3);
  await expect(inbox.locator(".inbox-footer")).toHaveText("3 reviews");

  // Facets combine with the text filter and persist per browser.
  await inbox.getByRole("button", { name: "Repository" }).click();
  await page
    .getByRole("menuitemcheckbox", { name: "acme-api", exact: true })
    .click();
  // Escape in a facet menu closes only the menu.
  await page.keyboard.press("Escape");
  await expect(inbox).toBeVisible();
  await expect(inbox.getByRole("link")).toHaveCount(2);
  await expect(inbox.locator(".inbox-footer")).toHaveText("2 reviews of 3");
  await inbox.getByRole("searchbox", { name: "Filter reviews" }).fill("auth");
  await expect(inbox.getByRole("link")).toHaveCount(1);
  await inbox
    .getByRole("searchbox", { name: "Filter reviews" })
    .press("Escape");
  await expect(inbox).toBeHidden();
  await expect(opener).toBeFocused();
  await expect(page.locator("main")).not.toHaveAttribute("inert", "");
  expect(await scroller.evaluate((element) => element.scrollTop)).toBe(top);

  await opener.click();
  await expect(inbox.getByRole("button", { name: "Repository" })).toHaveText(
    "Repository (1)",
  );
  await expect(inbox.getByRole("link")).toHaveCount(2);
  await inbox.getByRole("link", { name: /fix\/auth/ }).click();
  await expect(inbox).toBeHidden();
  await expect(page).toHaveURL(/\/streams\/stream-fix%2Fauth$/);
  await expect(identity).toContainText("fix/auth");
  await expect(nav.getByRole("link", { name: /^fix\/auth/ })).toBeFocused();
});

test("live stream URLs open the latest head on load and keep it until navigation", async ({
  page,
}) => {
  await page.addInitScript(() => {
    class IngestionEvents extends EventTarget {
      notify = () =>
        this.dispatchEvent(new MessageEvent("ingestion", { data: "{}" }));
      constructor() {
        super();
        window.addEventListener("test-ingestion", this.notify);
      }
      close() {
        window.removeEventListener("test-ingestion", this.notify);
      }
    }
    Object.defineProperty(window, "EventSource", { value: IngestionEvents });
  });
  const state = await localWorkspace(page);
  const snapshot = (id: string, lastSubmittedAt: number): ApiContext => ({
    ...state.context,
    id,
    streamId: "stream-main",
    latestContextId: id,
    lastSubmittedAt,
  });
  const contexts = new Map(
    [snapshot("main-1", 10), snapshot("main-2", 20)].map((context) => [
      context.id,
      context,
    ]),
  );
  let heads = [snapshot("main-1", 10)];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: heads, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    return route.fulfill({ json: contexts.get(id) });
  });
  const opened: string[] = [];
  page.on("request", (request) => {
    const match = /\/api\/v2\/contexts\/([^/?]+)$/.exec(
      new URL(request.url()).pathname,
    );
    if (match?.[1]) opened.push(match[1]);
  });

  await page.goto("/");
  await ready(page);
  await expect(page).toHaveURL(/\/streams\/stream-main$/);
  expect(opened.at(-1)).toBe("main-1");

  heads = [snapshot("main-2", 20)];
  await page.evaluate(() => window.dispatchEvent(new Event("test-ingestion")));
  await expect(
    page.getByRole("status").filter({ hasText: "Newer snapshot" }),
  ).toContainText("Newer snapshot");
  expect(opened.at(-1)).toBe("main-1");

  await page.reload();
  await ready(page);
  await expect(page).toHaveURL(/\/streams\/stream-main$/);
  expect(opened.at(-1)).toBe("main-2");
  await expect(
    page.getByRole("status").filter({ hasText: "Newer snapshot" }),
  ).toBeHidden();

  await page.goto("/contexts/main-1");
  await ready(page);
  await expect(page).toHaveURL(/\/contexts\/main-1$/);
  expect(opened.at(-1)).toBe("main-1");

  heads = [];
  await page.goto("/streams/stream-main");
  await expect(
    page.getByRole("heading", { name: "No current snapshot" }),
  ).toBeVisible();
});

for (const mode of ["local", "remote"]) {
  test(`${mode} global new changes lists unread streams to open explicitly`, async ({
    page,
  }) => {
    await page.clock.install();
    await page.addInitScript(() => {
      class IngestionEvents extends EventTarget {
        notify = () =>
          this.dispatchEvent(new MessageEvent("ingestion", { data: "{}" }));
        constructor() {
          super();
          window.addEventListener("test-ingestion", this.notify);
        }
        close() {
          window.removeEventListener("test-ingestion", this.notify);
        }
      }
      Object.defineProperty(window, "EventSource", { value: IngestionEvents });
    });
    const state = await localWorkspace(page);
    await page.route("**/api/v2/bootstrap", (route) =>
      route.fulfill({
        json: {
          mode,
          signupEnabled: false,
          user:
            mode === "remote" ? { id: "account-a", username: "alice" } : null,
          csrfToken: mode === "remote" ? "csrf" : null,
          collect: mode === "local",
        },
      }),
    );
    let collections = 0;
    await page.route("**/api/v2/collections*", (route) => {
      collections++;
      return route.fulfill({ json: { contexts: heads, failures: [] } });
    });
    const snapshot = (
      id: string,
      repository: string,
      activity: number,
      arrival: number,
    ): ApiContext => ({
      ...state.context,
      id,
      repositoryId: repository,
      name: repository,
      streamId: `stream-${repository}`,
      latestContextId: id,
      lastChangedAt: activity,
      lastSubmittedAt: arrival,
    });
    const viewed = snapshot("viewed", "api", 100, 1);
    const olderWeb = snapshot("web-old", "web", 50, 2);
    const contexts = new Map(
      [viewed, olderWeb].map((context) => [context.id, context]),
    );
    let heads = [olderWeb, viewed];
    await page.route("**/api/v2/contexts?*", (route) =>
      route.fulfill({ json: { contexts: heads, nextCursor: null } }),
    );
    await page.route("**/api/v2/contexts/*", (route) =>
      route.fulfill({
        json: contexts.get(
          new URL(route.request().url()).pathname.split("/").at(-1) ?? "",
        ),
      }),
    );
    await page.goto("/contexts/viewed");
    await ready(page);
    const global = page.getByRole("button", { name: /^New changes/ });
    await expect(global).toHaveCount(0);
    const nav = page.getByRole("navigation", { name: "Reviews", exact: true });
    await expect(nav).toBeVisible();
    await expect(nav.getByRole("listitem").getByRole("button")).toHaveText([
      /^api/,
      /^web/,
    ]);

    const newerApi = snapshot("api-new", "api", 200, 900);
    const newerWeb = {
      ...snapshot("web-new", "web", 400, 10),
      branch: "fix-login",
      worktreeName: "web-login",
    };
    contexts.set(newerApi.id, newerApi);
    contexts.set(newerWeb.id, newerWeb);
    heads = [newerApi, newerWeb];
    await page.evaluate(() =>
      window.dispatchEvent(new Event("test-ingestion")),
    );
    await expect(global).toHaveAccessibleName("New changes (2 streams)");
    await expect(global).toHaveText("2 new");
    await expect(nav.getByRole("listitem").getByRole("button")).toHaveText([
      /^web/,
      /^api/,
    ]);
    await expect(
      page.getByRole("status").filter({ hasText: "Newer snapshot" }),
    ).toContainText("Newer snapshot");
    await expect(page).toHaveURL(/\/contexts\/viewed$/);
    await page.clock.fastForward(5 * 60_000);
    expect(collections).toBe(0);

    // Opening the list never swaps the review; each entry names its checkout.
    await global.click();
    const menu = page.getByRole("menu", { name: "New changes" });
    await expect(menu).toBeVisible();
    await expect(page).toHaveURL(/\/contexts\/viewed$/);
    const items = menu.getByRole("menuitem");
    await expect(items).toHaveCount(2);
    await expect(items.nth(0)).toHaveAccessibleName(
      /^web, fix-login, web-login, /,
    );
    await expect(items.nth(0)).toContainText("web");
    await expect(items.nth(0)).toContainText("fix-login");
    await expect(items.nth(0)).toContainText("web-login");
    await expect(items.nth(1)).toHaveAccessibleName(/^api, main, /);

    // Choosing the less active entry opens exactly that stream and clears it.
    await items.nth(1).click();
    await ready(page);
    await expect(page).toHaveURL(/\/streams\/stream-api$/);
    await expect(menu).toBeHidden();
    await expect(global).toHaveAccessibleName("New changes (1 stream)");
    await expect(
      page.getByRole("status").filter({ hasText: "Newer snapshot" }),
    ).toBeHidden();
    await expect(nav.getByRole("link", { name: /^main/ })).toHaveAttribute(
      "aria-current",
      "page",
    );
    await expect(nav.getByRole("link", { name: /^main/ })).toBeFocused();

    // Compact chrome keeps the list keyboard reachable and restores focus to the closed drawer's toggle.
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ colorScheme: "dark" });
    await expect(global).toBeVisible();
    const bounds = await global.boundingBox();
    expect(bounds && bounds.x >= 0 && bounds.x + bounds.width <= 390).toBe(
      true,
    );
    await global.focus();
    await page.keyboard.press("Enter");
    await expect(menu).toBeVisible();
    const menuBounds = await menu.boundingBox();
    expect(
      menuBounds && menuBounds.x >= 0 && menuBounds.x + menuBounds.width <= 390,
    ).toBe(true);
    await expect(items).toHaveCount(1);
    await expect(items.first()).toBeFocused();
    await page.keyboard.press("Enter");
    await ready(page);
    await expect(page).toHaveURL(/\/streams\/stream-web$/);
    await expect(global).toHaveCount(0);
    await expect(
      page.getByRole("button", { name: "Show file sidebar" }),
    ).toBeFocused();
    // A new submission of the same head may reorder projects, but adds no unread content.
    heads = [
      { ...newerApi, lastSubmittedAt: 1000, lastChangedAt: 500 },
      newerWeb,
    ];
    await page.evaluate(() =>
      window.dispatchEvent(new Event("test-ingestion")),
    );
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.emulateMedia({ colorScheme: "light" });
    await expect(nav.getByRole("listitem").getByRole("button")).toHaveText([
      /^api/,
      /^web/,
    ]);
    await expect(global).toHaveCount(0);
    expect(collections).toBe(0);

    // Root selection shares activity ordering for stored and collected heads.
    heads = [newerWeb, { ...newerApi, lastChangedAt: 500 }];
    await page.goto("/");
    await ready(page);
    await expect(page).toHaveURL(/\/streams\/stream-api$/);
    expect(collections).toBe(mode === "local" ? 1 : 0);
  });
}

test("local first launch waits for collection with an empty catalog", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  const snapshot = {
    ...state.context,
    streamId: "local-stream",
    latestContextId: state.context.id,
  };
  let heads: ApiContext[] = [];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: heads, nextCursor: null } }),
  );
  let collections = 0;
  let pending: Route | undefined;
  await page.route("**/api/v2/collections*", (route) => {
    collections++;
    expect(new URL(route.request().url()).searchParams.get("initial")).toBe(
      "true",
    );
    pending = route;
  });

  await page.goto("/");
  const status = page.locator("main.session-status");
  await expect(status).toContainText("Collecting latest changes");
  await expect(status).toHaveAttribute("aria-busy", "true");
  await expect(status.locator(".diff-skeleton")).toBeVisible();
  const loadingBounds = await status.boundingBox();
  if (!loadingBounds) throw new Error("Missing loading workspace");
  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(
    await status
      .locator('[data-slot="skeleton"]')
      .first()
      .evaluate((element) => getComputedStyle(element).animationName),
  ).toBe("none");
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.locator(".loading-sidebar")).toBeHidden();
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= innerWidth,
    ),
  ).toBe(true);
  await page.setViewportSize({ width: 1280, height: 720 });
  // The review column shows its own placeholders beside the file tree's.
  const projects = page.getByRole("navigation", {
    name: "Reviews",
    exact: true,
  });
  await expect(projects).toContainText("Collecting reviews…");
  await expect(projects.locator(".projects-skeleton")).toBeVisible();
  await expect(page.locator(".loading-sidebar")).toBeVisible();
  for (const name of ["Resize reviews column", "Resize file sidebar"])
    await expect(page.getByRole("separator", { name })).toBeVisible();
  await expect(projects).not.toContainText("No current changes");
  await expect(projects).not.toContainText("Agent hooks and diffx sync");
  await expect(
    page.getByRole("heading", { name: "No review contexts yet" }),
  ).toHaveCount(0);
  await expect(page.locator("#viewer")).toHaveCount(0);
  await expect.poll(() => collections).toBe(1);
  if (!pending) throw new Error("Missing startup collection");
  heads = [snapshot];
  await pending.fulfill({
    json: { contexts: heads, failures: [], defaultContextId: snapshot.id },
  });
  await ready(page);
  await expect(page).toHaveURL(/\/streams\/local-stream$/);
  expect(collections).toBe(1);
  await expect(page.locator(".session-status")).toHaveCount(0);
  const reviewBounds = await page.locator(".workspace").boundingBox();
  if (!reviewBounds) throw new Error("Missing loaded workspace");
  expect(reviewBounds.x).toBeCloseTo(loadingBounds.x, 0);
  expect(reviewBounds.width).toBeCloseTo(loadingBounds.width, 0);
});

for (const initialHeads of ["stored", "arriving"]) {
  test(`local ${initialHeads} snapshots open during collection and preserve later navigation`, async ({
    page,
  }) => {
    const state = await localWorkspace(page);
    await page.addInitScript(() => {
      class IngestionEvents extends EventTarget {
        notify = () =>
          this.dispatchEvent(new MessageEvent("ingestion", { data: "{}" }));
        constructor() {
          super();
          window.addEventListener("test-ingestion", this.notify);
        }
        close() {
          window.removeEventListener("test-ingestion", this.notify);
        }
      }
      Object.defineProperty(window, "EventSource", { value: IngestionEvents });
    });
    await page.route("**/api/v2/bootstrap", (route) =>
      route.fulfill({
        json: {
          mode: "local",
          signupEnabled: false,
          user: null,
          csrfToken: null,
          collect: true,
        },
      }),
    );
    const snapshot = {
      ...state.context,
      streamId: "local-stream",
      latestContextId: state.context.id,
    };
    const pinned = { ...state.context, id: "pinned", name: "Pinned review" };
    let heads = initialHeads === "stored" ? [snapshot] : [];
    await page.route("**/api/v2/contexts?*", (route) =>
      route.fulfill({ json: { contexts: heads, nextCursor: null } }),
    );
    const opened: string[] = [];
    await page.route("**/api/v2/contexts/*", (route) => {
      const id =
        new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
      opened.push(id);
      return route.fulfill({ json: id === "pinned" ? pinned : snapshot });
    });
    let pending: Route | undefined;
    await page.route("**/api/v2/collections*", (route) => {
      pending = route;
    });
    await page.goto("/streams/local-stream");
    await expect.poll(() => pending !== undefined).toBe(true);
    if (initialHeads === "arriving") {
      await expect(page.locator("main.session-status")).toHaveAttribute(
        "aria-busy",
        "true",
      );
      heads = [snapshot];
      await page.evaluate(() =>
        window.dispatchEvent(new Event("test-ingestion")),
      );
    }
    await ready(page);
    await expect(
      page.getByRole("button", { name: "Sync all checkouts" }),
    ).toHaveAttribute("aria-busy", "true");
    await expect(page.locator(".session-status")).toHaveCount(0);
    // Same-document navigation must survive the earlier collection completing.
    await page.evaluate(() => {
      window.history.pushState(null, "", "/contexts/pinned");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    await expect(page.locator(".context-identity-name")).toHaveText(
      "Pinned review",
    );
    await ready(page);
    if (!pending) throw new Error("Missing initial collection");
    await pending.fulfill({
      json: {
        contexts: [{ ...snapshot, id: "newer", latestContextId: "newer" }],
        failures: [],
      },
    });
    await expect(
      page.getByRole("button", { name: "Sync all checkouts" }),
    ).toHaveAttribute("aria-busy", "false");
    await expect(page).toHaveURL(/\/contexts\/pinned$/);
    expect(opened).toEqual(["local-review", "pinned"]);
    await ready(page);
  });
}

test("local root waits for collection to open its default over earlier heads", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  const snapshot = (id: string, activity: number): ApiContext => ({
    ...state.context,
    id,
    streamId: `${id}-stream`,
    latestContextId: id,
    lastChangedAt: activity,
  });
  // The first checkouts to finish are visible before the launched checkout.
  const active = snapshot("active", 900);
  const launched = snapshot("launched", 100);
  let heads = [active];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: heads, nextCursor: null } }),
  );
  const opened: string[] = [];
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    opened.push(id);
    return route.fulfill({ json: id === "active" ? active : launched });
  });
  let pending: Route | undefined;
  await page.route("**/api/v2/collections*", (route) => {
    pending = route;
  });

  await page.goto("/");
  await expect.poll(() => pending !== undefined).toBe(true);
  const status = page.locator("main.session-status");
  await expect(status).toContainText("Collecting latest changes");
  await expect(status).toHaveAttribute("aria-busy", "true");
  await expect(
    page
      .getByRole("navigation", { name: "Reviews", exact: true })
      .getByRole("link"),
  ).toHaveCount(1);
  await expect(page).toHaveURL(/\/$/);
  expect(opened).toEqual([]);
  if (!pending) throw new Error("Missing initial collection");
  heads = [active, launched];
  await pending.fulfill({
    json: { contexts: heads, failures: [], defaultContextId: launched.id },
  });
  await ready(page);
  await expect(page).toHaveURL(/\/streams\/launched-stream$/);
  expect(opened).toEqual(["launched"]);
});

test("unresolved navigation joins collection without reporting a missing stream", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: [], nextCursor: null } }),
  );
  const snapshot = {
    ...state.context,
    streamId: "local-stream",
    latestContextId: state.context.id,
  };
  await page.route("**/api/v2/contexts/*", (route) =>
    route.fulfill({ json: snapshot }),
  );
  let collections = 0;
  let pending: Route | undefined;
  await page.route("**/api/v2/collections*", (route) => {
    collections++;
    pending = route;
  });
  await page.goto("/");
  await expect.poll(() => collections).toBe(1);
  await page.evaluate(() => {
    window.history.pushState(null, "", "/streams/local-stream");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  const status = page.locator("main.session-status");
  await expect(status).toHaveAttribute("aria-busy", "true");
  await expect(status).toContainText("Collecting latest changes");
  if (!pending) throw new Error("Missing initial collection");
  await pending.fulfill({ json: { contexts: [snapshot], failures: [] } });
  await ready(page);
  await expect(page).toHaveURL(/\/streams\/local-stream$/);
  await expect(
    page.getByRole("button", { name: "Sync all checkouts" }),
  ).toHaveAttribute("aria-busy", "false");
  expect(collections).toBe(1);
});

test("file previews render independently while other previews are pending", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/contexts/*/diffs/current?*", (route) => {
    const repository = state.repository("all");
    const file = repository.files[0];
    if (!file) throw new Error("Missing fixture file");
    return route.fulfill({
      json: {
        ...repository,
        files: [file, { ...file, id: "slow-file", path: "src/slow.ts" }],
      },
    });
  });
  let pending: Route | undefined;
  await page.route("**/files/slow-file/patch?*", (route) => {
    pending = route;
  });
  await page.goto("/contexts/local-review");
  await ready(page);
  await expect.poll(() => pending !== undefined).toBe(true);
  const refresh = page.getByRole("button", { name: "Refresh changes" });
  await expect(refresh).toHaveAttribute("aria-busy", "true");
  await expect(page.getByText("Loading diff…", { exact: true })).toBeVisible();
  if (!pending) throw new Error("Missing held preview");
  await pending.fulfill({ json: { patch: "", message: "Slow preview ready" } });
  await expect(
    page.getByText("Slow preview ready", { exact: true }),
  ).toBeVisible();
  await expect(refresh).toHaveAttribute("aria-busy", "false");
  await ready(page);
});

test("stored manifest loading keeps the diff surface mounted until content arrives", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  state.holdScopes.add("all");
  await page.goto("/contexts/local-review");
  const loading = page.locator('#empty[aria-busy="true"]');
  await expect(loading.locator(".diff-skeleton")).toBeVisible();
  await expect(page.locator("#viewer")).toHaveCount(1);
  await expect.poll(() => state.pendingScopes.length).toBe(1);
  const held = state.pendingScopes.shift();
  if (!held) throw new Error("Missing held manifest request");
  await held.route.fulfill({ json: held.repository });
  await ready(page);
  await expect(page.locator("#empty")).toBeHidden();
  await expect(page.locator("#viewer")).toHaveCount(1);
});

for (const path of ["/", "/streams/local-stream"]) {
  test(`local startup collection failure retries from ${path} without stored reviews`, async ({
    page,
  }) => {
    const state = await localWorkspace(page);
    await page.route("**/api/v2/bootstrap", (route) =>
      route.fulfill({
        json: {
          mode: "local",
          signupEnabled: false,
          user: null,
          csrfToken: null,
          collect: true,
        },
      }),
    );
    let heads: ApiContext[] = [];
    await page.route("**/api/v2/contexts?*", (route) =>
      route.fulfill({ json: { contexts: heads, nextCursor: null } }),
    );
    let collections = 0;
    let pending: Route | undefined;
    await page.route("**/api/v2/collections*", (route) => {
      collections++;
      if (collections === 1)
        return route.fulfill({
          status: 422,
          json: { detail: "Unable to collect checkouts: git failed" },
        });
      pending = route;
    });
    await page.goto(path);
    const status = page.locator("main.session-status");
    await expect(status).toContainText("Cannot sync changes");
    await expect(status).toContainText("git failed");
    await expect(status).toHaveAttribute("aria-busy", "false");
    await expect(status.locator(".diff-skeleton")).toHaveCount(0);
    await status.getByRole("button", { name: "Retry", exact: true }).click();
    await expect(status).toContainText("Collecting latest changes");
    await expect(status).toHaveAttribute("aria-busy", "true");
    await expect.poll(() => collections).toBe(2);
    if (!pending) throw new Error("Missing retried collection");
    heads = [{ ...state.context, streamId: "local-stream" }];
    await pending.fulfill({
      json: {
        contexts: heads,
        failures: [],
        defaultContextId: state.context.id,
      },
    });
    await ready(page);
    await expect(page).toHaveURL(/\/streams\/local-stream$/);
    await expect(page.getByRole("alert")).toHaveCount(0);
    expect(collections).toBe(2);
  });
}

test("local startup reports failed checkouts while opening collected changes", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: [], nextCursor: null } }),
  );
  await page.route("**/api/v2/collections*", (route) =>
    route.fulfill({
      json: {
        contexts: [{ ...state.context, streamId: "local-stream" }],
        failures: [{ path: "/workspace/broken", detail: "git failed" }],
        defaultContextId: state.context.id,
      },
    }),
  );
  await page.goto("/");
  await ready(page);
  await expect(page).toHaveURL(/\/streams\/local-stream$/);
  await expect(page.getByRole("alert")).toContainText(
    "Unable to sync 1 checkout: /workspace/broken (git failed)",
  );
});

test("local live URLs sync the checkouts on every load", async ({ page }) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  const snapshot = (id: string): ApiContext => ({
    ...state.context,
    id,
    streamId: "local-stream",
    latestContextId: id,
  });
  let heads = [snapshot("stored")];
  const opened: string[] = [];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: heads, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    opened.push(id);
    return route.fulfill({ json: snapshot(id) });
  });
  let collected = 0;
  let release = () => {};
  const held = new Promise<void>((resolve) => {
    release = resolve;
  });
  let failNext = false;
  await page.route("**/api/v2/collections*", async (route) => {
    collected++;
    if (collected === 1) await held;
    if (failNext)
      return route.fulfill({
        status: 422,
        json: { detail: "Unable to collect checkouts: git failed" },
      });
    // Live URLs open their own stream, not the most active one.
    const other = {
      ...snapshot(`other-${collected}`),
      streamId: "other",
      lastChangedAt: 100,
      lastSubmittedAt: 900,
    };
    heads = [
      { ...snapshot(`collected-${collected}`), lastChangedAt: 50 },
      other,
    ];
    return route.fulfill({
      json: { contexts: heads, failures: [], defaultContextId: other.id },
    });
  });

  await page.goto("/streams/local-stream");
  await ready(page);
  await expect(
    page.getByRole("button", { name: "Sync all checkouts" }),
  ).toHaveAttribute("aria-busy", "true");
  await expect(page.locator(".session-status")).toHaveCount(0);
  await expect.poll(() => state.scopes.length).toBe(1);
  release();
  await expect(
    page.getByRole("button", { name: "Sync all checkouts" }),
  ).toHaveAttribute("aria-busy", "false");
  await expect(page).toHaveURL(/\/streams\/local-stream$/);
  expect(collected).toBe(1);
  expect(opened).toEqual(["stored"]);
  await expect(
    page.getByRole("status").filter({ hasText: "Newer snapshot" }),
  ).toContainText("Newer snapshot");
  await page.getByRole("button", { name: "View latest" }).click();
  await expect.poll(() => opened.at(-1)).toBe("collected-1");
  await ready(page);
  expect(collected).toBe(1);

  // Collapse state belongs to the stream, so it survives the new snapshot.
  const viewer = page.locator("#viewer");
  await fileToggle(viewer).first().click();
  await page.reload();
  await expect(fileToggle(viewer, { expanded: false })).toHaveCount(1);
  expect(collected).toBe(2);
  await expect(page).toHaveURL(/\/streams\/local-stream$/);
  await fileToggle(viewer).first().click();

  await page.goto("/contexts/local-review");
  await ready(page);
  expect(collected).toBe(2);

  // A failed collection falls back to the stream's stored head.
  failNext = true;
  await page.goto("/streams/local-stream");
  await ready(page);
  await expect(
    page.getByRole("alert").filter({ hasText: "git failed" }),
  ).toBeVisible();
  expect(collected).toBe(3);
});

test("local sync collects every checkout without replacing the open review", async ({
  page,
}) => {
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  const snapshot = (id: string, streamId: string): ApiContext => ({
    ...state.context,
    id,
    streamId,
    latestContextId: id,
  });
  let heads = [snapshot("local-review", "local-stream")];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: heads, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    return route.fulfill({ json: snapshot(id, "local-stream") });
  });
  const collections: string[] = [];
  let fail = false;
  await page.route("**/api/v2/collections*", (route) => {
    collections.push(route.request().method());
    expect(new URL(route.request().url()).searchParams.get("initial")).toBe(
      "false",
    );
    if (fail)
      return route.fulfill({
        status: 422,
        json: { detail: "Unable to collect checkouts: git failed" },
      });
    heads = [
      snapshot("synced", "local-stream"),
      snapshot("worktree", "worktree-stream"),
    ];
    return route.fulfill({
      json: {
        contexts: heads,
        failures: [{ path: "/work/broken", detail: "git failed" }],
      },
    });
  });
  await page.goto("/contexts/local-review");
  await ready(page);
  await expect(
    page.getByRole("button", {
      name: /^(Refresh|Retry loading) changes$/,
      includeHidden: true,
    }),
  ).toHaveCount(0);
  const sync = page.getByRole("button", { name: "Sync all checkouts" });
  await sync.hover();
  const tooltip = page.locator('[data-slot="tooltip-content"]');
  await expect(tooltip).toContainText("Sync all checkouts");
  await expect(tooltip.getByText("Alt+R", { exact: true })).toBeVisible();
  await sync.click();
  await expect(page.locator("#comment-feedback")).toHaveText(
    "Synced 3 checkouts · 2 updated · 1 failed: /work/broken (git failed)",
  );
  // Like distributed arrivals, the newer head is announced, never swapped in.
  await expect(
    page.getByRole("status").filter({ hasText: "Newer snapshot" }),
  ).toContainText("Newer snapshot");
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
  expect(collections).toEqual(["POST"]);
  await page.keyboard.press("Alt+r");
  await expect(page.locator("#comment-feedback")).toHaveText(
    "Synced 3 checkouts · 0 updated · 1 failed: /work/broken (git failed)",
  );
  fail = true;
  await sync.click();
  await expect(
    page.getByRole("alert").filter({ hasText: "git failed" }),
  ).toBeVisible();
  await expect(page).toHaveURL(/\/contexts\/local-review$/);
  expect(collections).toHaveLength(3);
});

test("each remote account baselines its own seen heads", async ({ page }) => {
  // Another account's seen map in this browser must not mark this account's heads unread.
  await page.addInitScript(() => {
    const other = JSON.stringify({ "stream-other": "other-head" });
    localStorage.setItem("projects-seen", other);
    localStorage.setItem("projects-seen:account-a", other);
  });
  const state = await localWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "remote",
        signupEnabled: false,
        user: { id: "account-b", username: "bob" },
        csrfToken: "csrf",
        collect: false,
      },
    }),
  );
  const head = (repository: string): ApiContext => ({
    ...state.context,
    id: `${repository}-head`,
    repositoryId: repository,
    name: repository,
    streamId: `stream-${repository}`,
    latestContextId: `${repository}-head`,
    lastChangedAt: 100,
    lastSubmittedAt: 1,
  });
  const heads = [head("api"), head("web")];
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: heads, nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) =>
    route.fulfill({
      json: heads.find((context) =>
        new URL(route.request().url()).pathname.endsWith(`/${context.id}`),
      ),
    }),
  );
  await page.goto("/contexts/api-head");
  await ready(page);
  await expect(
    page
      .getByRole("navigation", { name: "Reviews", exact: true })
      .getByRole("listitem")
      .getByRole("button"),
  ).toHaveText([/^api/, /^web/]);
  await expect(page.getByRole("button", { name: /^New changes/ })).toHaveCount(
    0,
  );
  expect(
    await page.evaluate(() => localStorage.getItem("projects-seen:account-b")),
  ).toContain("web-head");
});

async function capturedWorkspace(page: Page) {
  const state = await localWorkspace(page);
  state.context.streamId = "captured-stream";
  state.context.capabilities.diff.scopes.values = [
    "all",
    "staged",
    "unstaged",
    "uncommitted",
    "committed",
  ];
  state.context.scopeSummaries = [
    { scope: "all", comparisonKind: "combined", fileCount: 1 },
    { scope: "staged", comparisonKind: "staged", fileCount: 1 },
    { scope: "unstaged", comparisonKind: "unstaged", fileCount: 1 },
    { scope: "uncommitted", comparisonKind: "uncommitted", fileCount: 1 },
    { scope: "committed", comparisonKind: "committed", fileCount: 1 },
  ];
  return state;
}

async function chooseComparison(page: Page, label: string) {
  await page.getByRole("button", { name: /^Comparison:/ }).click();
  await page.getByRole("menuitemradio", { name: label, exact: true }).click();
  await expect(
    page.getByRole("menuitemradio", { name: label, exact: true }),
  ).toBeHidden();
}

test("captured comparison default, saved stream choice, and explicit URL precedence", async ({
  page,
}) => {
  const state = await capturedWorkspace(page);
  await page.goto("/contexts/local-review");
  await ready(page, "uncommitted-v1");
  expect(state.scopes).toEqual(["uncommitted"]);
  await expect(
    page.getByRole("group", { name: "Uncommitted scope" }).getByRole("button"),
  ).toHaveText(["All", "Staged", "Unstaged"]);
  await chooseComparison(page, "Branch changes");
  await ready(page, "committed-v1");
  await showDiff(page);
  await expect(
    page.locator('#file-tree [data-path="src/value.ts"]'),
  ).toHaveAttribute("aria-label", "src/value.ts, Modified");
  await expect(page.locator(".git-legend")).toBeHidden();
  await page.reload();
  await ready(page, "committed-v1");
  await page.goto("/contexts/local-review");
  await ready(page, "committed-v1");
  await page.goto("/contexts/local-review?scope=staged");
  await ready(page, "staged-v1");
  await page.evaluate(() => sessionStorage.clear());
  state.context.scopeSummaries =
    state.context.scopeSummaries?.map((summary) => ({
      ...summary,
      fileCount: summary.comparisonKind === "committed" ? 1 : 0,
    })) ?? [];
  await page.goto("/contexts/local-review");
  await ready(page, "committed-v1");
});

test("scope history keeps live snapshot and draft through newer heads, then follows cross-route navigation", async ({
  page,
}) => {
  const state = await capturedWorkspace(page);
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "local",
        signupEnabled: false,
        user: null,
        csrfToken: null,
        collect: true,
      },
    }),
  );
  let collections = 0;
  let head: ApiContext = {
    ...state.context,
    id: "first",
    latestContextId: "first",
  };
  const opened: string[] = [];
  await page.route("**/api/v2/collections*", (route) => {
    collections++;
    return route.fulfill({
      json: { contexts: [head], failures: [], defaultContextId: head.id },
    });
  });
  await page.route("**/api/v2/contexts?*", (route) =>
    route.fulfill({ json: { contexts: [head], nextCursor: null } }),
  );
  await page.route("**/api/v2/contexts/*", (route) => {
    const id = new URL(route.request().url()).pathname.split("/").at(-1) ?? "";
    opened.push(id);
    return route.fulfill({ json: { ...head, id } });
  });
  await page.goto("/streams/captured-stream?scope=uncommitted");
  await ready(page, "uncommitted-v1");
  await expect.poll(() => collections).toBe(1);
  await page.getByRole("button", { name: "Staged", exact: true }).click();
  await ready(page, "staged-v1");
  head = { ...head, id: "second", latestContextId: "second", lastChangedAt: 2 };
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await page.getByRole("button", { name: "View latest" }).click();
  await ready(page, "staged-v1");
  expect(opened).toEqual(["first", "second"]);
  await page
    .getByRole("button", {
      name: "Leave review comment on file src/value.ts",
      exact: true,
    })
    .click();
  const draft = page
    .locator("#viewer")
    .getByRole("textbox", { name: "Review comment" });
  await draft.fill("Keep staged draft on captured second");
  await chooseComparison(page, "Branch changes");
  await ready(page, "committed-v1");
  head = { ...head, id: "third", latestContextId: "third", lastChangedAt: 3 };
  await page.evaluate(() =>
    document.dispatchEvent(new Event("visibilitychange")),
  );
  await page.goBack();
  await ready(page, "staged-v1");
  await expect(draft).toHaveValue("Keep staged draft on captured second");
  await page.goBack();
  await ready(page, "uncommitted-v1");
  await page.goForward();
  await ready(page, "staged-v1");
  await expect(draft).toHaveValue("Keep staged draft on captured second");
  await page.goForward();
  await ready(page, "committed-v1");
  expect(collections).toBe(1);
  expect(opened).toEqual(["first", "second"]);
  // A different route intentionally starts a new session. Returning follows
  // the stream's new head once; subsequent scope history stays there.
  await page.evaluate(() => {
    window.history.pushState(null, "", "/contexts/other?scope=all");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await ready(page, "all-v1");
  await page.goBack();
  await ready(page, "committed-v1");
  await expect.poll(() => collections).toBe(2);
  expect(opened).toEqual(["first", "second", "other", "third"]);
  await page.goBack();
  await ready(page, "staged-v1");
  expect(opened).toEqual(["first", "second", "other", "third"]);
  expect(collections).toBe(2);
});

test("unavailable explicit scope stays explained without requesting it, including stale responses", async ({
  page,
}) => {
  const state = await capturedWorkspace(page);
  state.context.capabilities.diff.scopes.values = ["all", "staged", "unstaged"];
  state.context.scopeSummaries = [
    { scope: "all", comparisonKind: "uncommitted", fileCount: 1 },
    { scope: "staged", comparisonKind: "staged", fileCount: 1 },
    { scope: "unstaged", comparisonKind: "unstaged", fileCount: 1 },
  ];
  await page.goto("/contexts/local-review?scope=committed");
  await expect(
    page.getByText("Comparison unavailable", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/The “committed” scope was not collected/),
  ).toBeVisible();
  expect(state.scopes).toEqual([]);
  await chooseComparison(page, "Uncommitted");
  await ready(page);
  state.holdScopes.add("staged");
  await page.getByRole("button", { name: "Staged", exact: true }).click();
  await expect.poll(() => state.pendingScopes.length).toBe(1);
  await page.goBack();
  await ready(page);
  await page.goBack();
  await expect(
    page.getByText("Comparison unavailable", { exact: true }),
  ).toBeVisible();
  const pending = state.pendingScopes.shift();
  if (!pending) throw new Error("Missing delayed staged response");
  await pending.route.fulfill({ json: pending.repository });
  await expect(
    page.getByText("Comparison unavailable", { exact: true }),
  ).toBeVisible();
  await expect(
    page
      .locator("#viewer")
      .getByText('export const value = "staged-v1";', { exact: true }),
  ).toHaveCount(0);
  expect(state.scopes).not.toContain("committed");
  await page.goto("/contexts/local-review?scope=unknown");
  await expect(
    page.getByText(/The “unknown” scope was not collected/),
  ).toBeVisible();
});

test("empty net comparisons expose staged and unstaged changes that cancel", async ({
  page,
}) => {
  const state = await capturedWorkspace(page);
  state.emptyScopes.add("uncommitted");
  state.emptyScopes.add("all");
  state.context.scopeSummaries =
    state.context.scopeSummaries?.map((summary) => ({
      ...summary,
      fileCount: state.emptyScopes.has(summary.scope) ? 0 : 1,
    })) ?? [];
  await page.goto("/contexts/local-review");
  await expect(
    page.getByText("No net uncommitted changes", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(/Captured changes are available in Staged, Unstaged/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Staged", exact: true }).click();
  await ready(page, "staged-v1");
  await chooseComparison(page, "Combined");
  await expect(
    page.getByText("No net combined changes", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText(
      /Captured changes are available in Staged, Unstaged, Branch changes/,
    ),
  ).toBeVisible();
});

test("saving a draft from another comparison uses its original scope and diff identity", async ({
  page,
}) => {
  await capturedWorkspace(page);
  let submitted: unknown;
  await page.route("**/api/v2/contexts/*/comments", (route) => {
    if (route.request().method() !== "POST")
      return route.fulfill({ json: { comments: [] } });
    submitted = route.request().postDataJSON();
    return route.fulfill({
      json: {
        id: "saved",
        diffId: "diff-uncommitted",
        versionId: "uncommitted-v1",
        path: "src/value.ts",
        scope: "uncommitted",
        fingerprint: "uncommitted-v1",
        target: "file",
        side: "additions",
        start: 0,
        end: 0,
        code: "",
        body: "Original scope",
        status: "open",
        createdAt: 1,
      },
    });
  });
  await page.goto("/contexts/local-review");
  await ready(page, "uncommitted-v1");
  await page
    .getByRole("button", {
      name: "Leave review comment on file src/value.ts",
      exact: true,
    })
    .click();
  await page
    .locator("#viewer")
    .getByRole("textbox", { name: "Review comment" })
    .fill("Original scope");
  await chooseComparison(page, "Branch changes");
  await ready(page, "committed-v1");
  await showDiff(page);
  await page.getByRole("tab", { name: /Review/ }).click();
  await page
    .locator("#comments-panel")
    .getByRole("button", { name: "Save comment" })
    .click();
  await expect
    .poll(() => submitted)
    .toEqual({
      diffId: "diff-uncommitted",
      versionId: "uncommitted-v1",
      fileId: "value-file",
      scope: "uncommitted",
      fileVersion: "uncommitted-v1",
      target: "file",
      side: "additions",
      start: 0,
      end: 0,
      body: "Original scope",
    });
});

test("saved comparison preferences belong to the active account", async ({
  page,
}) => {
  await capturedWorkspace(page);
  let account = "first-account";
  await page.route("**/api/v2/bootstrap", (route) =>
    route.fulfill({
      json: {
        mode: "remote",
        signupEnabled: false,
        user: { id: account, username: account },
        csrfToken: "test",
        collect: false,
      },
    }),
  );
  await page.goto("/contexts/local-review");
  await ready(page, "uncommitted-v1");
  await chooseComparison(page, "Branch changes");
  await ready(page, "committed-v1");
  account = "second-account";
  await page.goto("/contexts/local-review");
  await ready(page, "uncommitted-v1");
  account = "first-account";
  await page.goto("/contexts/local-review");
  await ready(page, "committed-v1");
});
