import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiContext, ApiPullRequest } from "@diffx/api";
import { contextActivity } from "../src/context-activity.ts";
import {
  adjacentReview,
  agentFacets,
  baselineOpened,
  benchList,
  benchOrder,
  benchPath,
  benchQuery,
  benchRows,
  benchSections,
  benchState,
  changedAreas,
  repositoryFacets,
  viewCounts,
  type BenchMarks,
  type BenchQuery,
} from "../src/review-bench.ts";

function head(
  id: string,
  repositoryId: string,
  activity: number,
  overrides: Partial<ApiContext> = {},
): ApiContext {
  return {
    id,
    kind: "observation",
    source: "local",
    streamId: `stream-${id}`,
    latestContextId: id,
    name: repositoryId,
    branch: `branch-${id}`,
    worktreeName: null,
    root: `/repos/${repositoryId}`,
    repositoryId,
    locationId: null,
    createdAt: activity,
    lastSubmittedAt: activity,
    lastChangedAt: activity,
    changedFileCount: 2,
    expiresAt: null,
    submittedFrom: null,
    availability: "available",
    stale: false,
    capabilities: {
      diff: {
        scopes: { state: "enabled", values: ["all"] },
        refresh: { state: "unavailable" },
        stagingMetadata: { state: "enabled" },
      },
      files: { contents: { state: "enabled" } },
      review: { comments: { state: "enabled" } },
    },
    ...overrides,
  };
}

function pull(
  id: string,
  activity: number,
  overrides: Partial<ApiPullRequest> = {},
): ApiContext {
  const pullRequest: ApiPullRequest = {
    provider: "github",
    repositoryId: 10,
    fullName: "octo/app",
    number: 12,
    url: "https://github.com/octo/app/pull/12",
    title: "Add search",
    body: "",
    state: "open",
    draft: false,
    author: { login: "mona", url: "https://github.com/mona" },
    baseRef: "main",
    headRef: "search",
    headRepository: "mona/app",
    createdAt: 1,
    updatedAt: 2,
    ...overrides,
  };
  return head(id, "app", activity, {
    observation: {
      sourceId: "github",
      hostname: "github.com",
      runId: "",
      agent: "",
      trigger: "github",
      repositoryKey: "app",
      repositoryName: "app",
      remoteUrl: "https://github.com/octo/app.git",
      checkoutKey: id,
      root: pullRequest.url,
      worktreeName: `PR #${pullRequest.number}`,
      branch: "search",
      head: "a".repeat(40),
      collectedAt: activity,
      collectorVersion: "test",
      pullRequest,
    },
  });
}

function agent(id: string, repositoryId: string, harness: string) {
  return head(id, repositoryId, 50, {
    sessions: [
      {
        sourceId: "source",
        harness,
        id: `${id}-session`,
        firstObservedAt: 1,
        lastObservedAt: 1,
      },
    ],
  });
}

const empty: BenchMarks = { opened: {}, viewed: {} };
const query = (overrides: Partial<BenchQuery> = {}): BenchQuery => ({
  view: "attention",
  repo: "",
  agent: "",
  q: "",
  ...overrides,
});

test("bench state: closed and drafts first, then opened or viewed heads", () => {
  const work = head("work", "api", 10);
  assert.equal(benchState(work, empty), "new");
  assert.equal(
    benchState(work, { opened: { "stream-work": "work" }, viewed: {} }),
    "viewed",
  );
  assert.equal(
    benchState(work, { opened: {}, viewed: { "stream-work": "work" } }),
    "viewed",
  );
  // A newer head in a stream the reader opened before is an update.
  assert.equal(
    benchState(work, { opened: { "stream-work": "older" }, viewed: {} }),
    "updated",
  );
  assert.equal(benchState({ ...work, stale: true }, empty), "closed");
  assert.equal(
    benchState(pull("merged", 1, { state: "merged" }), empty),
    "closed",
  );
  assert.equal(
    benchState(pull("closed", 1, { state: "closed" }), empty),
    "closed",
  );
  assert.equal(benchState(pull("draft", 1, { draft: true }), empty), "draft");
  // Until marks load nothing claims attention.
  assert.equal(benchState(work, null), "viewed");
});

test("views split attention, open and closed work; sections lead with unread", () => {
  const marks: BenchMarks = {
    opened: { "stream-seen": "seen" },
    viewed: {},
  };
  const rows = benchRows(
    [
      head("seen", "api", 40),
      head("fresh", "api", 30),
      pull("draft", 20, { draft: true }),
      pull("merged", 90, { state: "merged" }),
      head("clean", "api", 99, { changedFileCount: 0 }),
    ],
    marks,
  );
  assert.deepEqual(
    rows.map((row) => [row.id, row.state]),
    [
      ["stream-merged", "closed"],
      ["stream-seen", "viewed"],
      ["stream-fresh", "new"],
      ["stream-draft", "draft"],
    ],
  );
  assert.deepEqual(viewCounts(rows), { attention: 1, open: 3, closed: 1 });
  const open = benchSections(benchList(rows, query({ view: "open" })), "open");
  assert.deepEqual(
    open.map((section) => [section.title, section.rows.map((row) => row.id)]),
    [
      ["New and updated", ["stream-fresh"]],
      ["Everything else", ["stream-seen", "stream-draft"]],
    ],
  );
  assert.deepEqual(
    benchOrder(open).map((row) => row.id),
    ["stream-fresh", "stream-seen", "stream-draft"],
  );
  assert.deepEqual(
    benchSections(benchList(rows, query({ view: "closed" })), "closed").map(
      (section) => section.title,
    ),
    ["Closed"],
  );
  assert.deepEqual(benchSections([], "attention"), []);
});

test("facets count the current view; filters combine with search", () => {
  const rows = benchRows(
    [
      agent("a", "api", "claude"),
      agent("b", "api", "codex"),
      agent("c", "web", "claude"),
      head("d", "web", 10, { stale: true }),
    ],
    empty,
  );
  assert.deepEqual(repositoryFacets(rows, "open"), [
    { value: "api", label: "api", count: 2 },
    { value: "web", label: "web", count: 1 },
  ]);
  assert.deepEqual(agentFacets(rows, "open"), [
    { value: "claude", label: "claude", count: 2 },
    { value: "codex", label: "codex", count: 1 },
  ]);
  assert.deepEqual(
    benchList(rows, query({ repo: "api", agent: "claude" })).map(
      (row) => row.id,
    ),
    ["stream-a"],
  );
  assert.deepEqual(
    benchList(rows, query({ q: "web branch-c" })).map((row) => row.id),
    ["stream-c"],
  );
});

test("the bench query round-trips through the address without defaults", () => {
  assert.deepEqual(benchQuery(""), query());
  assert.deepEqual(benchQuery("?view=bogus"), query());
  assert.equal(benchPath(query()), "/");
  const filtered = query({
    view: "closed",
    repo: "api",
    agent: "codex",
    q: "x y",
  });
  assert.equal(benchPath(filtered), "/?view=closed&repo=api&agent=codex&q=x+y");
  assert.deepEqual(benchQuery(benchPath(filtered).slice(1)), filtered);
});

test("first visits count work idle for a day as opened", () => {
  const day = 24 * 60 * 60 * 1000;
  const now = 10 * day;
  const idle = head("idle", "api", now - day);
  const recent = head("recent", "api", now - day + 1);
  assert.deepEqual(baselineOpened([idle, recent], now, contextActivity), {
    "stream-idle": "idle",
  });
});

test("adjacent reviews follow the order and stop at its ends", () => {
  const order = ["a", "b", "c"];
  assert.equal(adjacentReview(order, "b", 1), "c");
  assert.equal(adjacentReview(order, "b", -1), "a");
  assert.equal(adjacentReview(order, "c", 1), undefined);
  assert.equal(adjacentReview(order, "z", 1), undefined);
});

test("changed areas group paths by their leading directories", () => {
  assert.deepEqual(
    changedAreas([
      "src/app/a.ts",
      "src/app/b.ts",
      "src/lib/c.ts",
      "README.md",
      "docs/guide.md",
    ]),
    [
      { area: "src/app", count: 2 },
      { area: "docs", count: 1 },
      { area: "README.md", count: 1 },
      { area: "src/lib", count: 1 },
    ],
  );
  assert.equal(changedAreas(["a/x", "b/x", "c/x"], 2).length, 2);
});
