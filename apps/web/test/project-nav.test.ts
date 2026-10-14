import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiContext } from "@diffx/api";
import {
  baselineSeen,
  newerHead,
  projectGroups,
  readSeen,
  relativeAge,
  searchProjectGroups,
  streamMeta,
} from "../src/project-nav.ts";

function head(
  id: string,
  repositoryId: string,
  lastSubmittedAt: number,
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
    createdAt: lastSubmittedAt,
    lastSubmittedAt,
    lastChangedAt: lastSubmittedAt,
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

test("projects list current changed streams by recent activity, piped last", () => {
  const groups = projectGroups(
    [
      head("old-api", "api", 10),
      head("web", "web", 30),
      head("new-api", "api", 20),
      head("clean", "api", 40, { changedFileCount: 0 }),
      head("stale", "api", 50, { stale: true }),
      head("gone", "web", 60, { availability: "unavailable" }),
      head("piped", "", 70, {
        source: "stdin",
        repositoryId: null,
        streamId: null,
      }),
    ],
    null,
  );
  assert.deepEqual(
    groups.map((group) => [
      group.name,
      group.streams.map((stream) => stream.context.id),
    ]),
    [
      ["web", ["web"]],
      ["api", ["new-api", "old-api"]],
      ["Piped", ["piped"]],
    ],
  );
  assert.equal(groups[1]?.lastActivity, 20);
});

test("project search keeps matching streams in order and drops empty projects", () => {
  const groups = projectGroups(
    [
      head("auth", "api", 30, { branch: "feature/auth" }),
      head("main", "api", 20, { branch: "main" }),
      head("web", "web", 10, { branch: "feature/auth-ui" }),
    ],
    null,
  );
  const ids = (query: string) =>
    searchProjectGroups(groups, query).map((group) => [
      group.name,
      group.streams.map((stream) => stream.context.id),
    ]);
  assert.equal(searchProjectGroups(groups, "  "), groups);
  assert.deepEqual(ids("auth"), [
    ["api", ["auth"]],
    ["web", ["web"]],
  ]);
  assert.deepEqual(ids("api main"), [["api", ["main"]]]);
  assert.deepEqual(ids("WEB"), [["web", ["web"]]]);
  assert.deepEqual(ids("missing"), []);
});

test("unseen marks follow the newest opened snapshot per stream", () => {
  const first = head("first", "api", 1);
  const seen = baselineSeen([first]);
  const update = head("second", "api", 2, { streamId: "stream-first" });
  const arrival = head("third", "api", 3);
  const [group] = projectGroups([update, arrival], seen);
  assert.deepEqual(
    group?.streams.map((stream) => [stream.context.id, stream.unseen]),
    [
      ["third", true],
      ["second", true],
    ],
  );
  const [baseline] = projectGroups([first], null);
  assert.equal(baseline?.streams[0]?.unseen, false);
  assert.equal(readSeen("not json"), null);
  assert.deepEqual(readSeen('{"a":"b","c":1}'), { a: "b" });
});

test("a viewed snapshot names its stream's newer head", () => {
  const viewed = head("viewed", "api", 1, { streamId: "shared" });
  const newer = head("newer", "api", 2, { streamId: "shared" });
  assert.equal(newerHead(viewed, [newer, head("other", "api", 3)]), newer);
  assert.equal(newerHead(newer, [newer]), undefined);
  assert.equal(newerHead({ ...viewed, streamId: null }, [newer]), undefined);
});

test("stream metadata names worktree, harness, host only when ambiguous, and age", () => {
  const now = 3 * 60 * 60_000;
  const context = head("work", "api", now - 5 * 60_000, {
    worktreeName: "wt-auth",
    observation: {
      sourceId: "source",
      hostname: "laptop",
      runId: "",
      agent: "",
      remoteUrl: "",
      head: null,
      repositoryKey: "repo",
      repositoryName: "api",
      checkoutKey: "checkout",
      linkedWorktree: true,
      root: "/repos/wt-auth",
      worktreeName: "wt-auth",
      branch: "auth",
      collectedAt: 1,
      collectorVersion: "test",
      trigger: "agent-hook",
    },
    sessions: [
      {
        sourceId: "source",
        harness: "codex",
        id: "a",
        firstObservedAt: 1,
        lastObservedAt: 1,
      },
      {
        sourceId: "source",
        harness: "claude",
        id: "b",
        firstObservedAt: 2,
        lastObservedAt: 2,
      },
    ],
  });
  assert.equal(streamMeta(context, false, now), "wt-auth · claude · 5m");
  assert.equal(
    streamMeta(context, true, now),
    "wt-auth · claude · laptop · 5m",
  );
  assert.equal(relativeAge(now, now), "now");
  assert.equal(relativeAge(0, now), "3h");
  assert.equal(relativeAge(0, 49 * 60 * 60_000), "2d");
});
