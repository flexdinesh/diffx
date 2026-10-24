import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiContext } from "@diffx/api";
import {
  baselineSeen,
  latestStream,
  newerHead,
  projectGroups,
  readSeen,
  recentStreams,
  relativeAge,
  repoMonogram,
  streamMatches,
  streamMeta,
} from "../src/project-nav.ts";
import {
  compareContextActivity,
  contextActivity,
  latestChangedContext,
} from "../src/context-activity.ts";

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

test("projects list current changed streams by recent activity", () => {
  const groups = projectGroups(
    [
      head("old-api", "api", 10),
      head("web", "web", 30),
      head("new-api", "api", 20),
      head("clean", "api", 40, { changedFileCount: 0 }),
      head("stale", "api", 50, { stale: true }),
      head("gone", "web", 60, { availability: "unavailable" }),
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
    ],
  );
  assert.equal(groups[1]?.lastActivity, 20);
});

test("a repository opens to its most active stream", () => {
  const [api] = projectGroups(
    [
      head("quiet", "api", 10, { lastChangedAt: 10 }),
      head("busy", "api", 5, { lastChangedAt: 90 }),
    ],
    null,
  );
  assert.ok(api);
  assert.equal(latestStream(api)?.context.id, "busy");
});

test("activity orders streams and repositories independently of submission time", () => {
  const active = head("active", "api", 10, { lastChangedAt: 300 });
  const submitted = head("submitted", "web", 500, { lastChangedAt: 100 });
  const groups = projectGroups(
    [submitted, head("old", "api", 900, { lastChangedAt: 50 }), active],
    null,
  );
  assert.deepEqual(
    groups.map((group) => [
      group.name,
      group.streams.map((stream) => stream.context.id),
    ]),
    [
      ["api", ["active", "old"]],
      ["web", ["submitted"]],
    ],
  );
  assert.equal(latestChangedContext([submitted, active]), active);
  assert.equal(latestChangedContext([active, submitted]), active);
  assert.equal(
    latestChangedContext([
      { ...active, stale: true },
      { ...submitted, changedFileCount: 0 },
    ]),
    undefined,
  );
  assert.equal(
    streamMeta({ ...active, lastChangedAt: 60_000 }, false, 360_000),
    "5m",
  );
});

test("missing activity uses captured time, never submission arrival", () => {
  const context = head("saved", "api", 900, {
    lastChangedAt: 0,
    createdAt: 20,
  });
  assert.equal(contextActivity(context), 20);
  const observed = head("observed", "api", 1, {
    worktreeName: "wt",
    observation: {
      sourceId: "source",
      hostname: "host",
      runId: "",
      agent: "",
      remoteUrl: "",
      head: null,
      repositoryKey: "repo",
      repositoryName: "api",
      checkoutKey: "checkout",
      linkedWorktree: false,
      root: "/repo",
      worktreeName: "wt",
      branch: "main",
      collectedAt: 40,
      collectorVersion: "test",
      trigger: "manual",
    },
    lastChangedAt: 0,
  });
  assert.equal(contextActivity(observed), 40);
  assert.equal(latestChangedContext([context, observed]), observed);
});

test("activity ties use stream then context identity, including same-name projects", () => {
  const contexts = [
    head("b", "repo-b", 10, { name: "same", streamId: "stream-a" }),
    head("a", "repo-b", 20, {
      name: "same",
      lastChangedAt: 10,
      streamId: "stream-a",
    }),
    head("c", "repo-a", 10, { name: "same", streamId: "stream-z" }),
  ];
  assert.deepEqual(
    [...contexts].sort(compareContextActivity).map((context) => context.id),
    ["a", "b", "c"],
  );
  for (const heads of [contexts, [...contexts].reverse()]) {
    assert.equal(latestChangedContext(heads)?.id, "a");
    assert.deepEqual(
      projectGroups(heads, null).map((group) => group.id),
      ["repo-b", "repo-a"],
    );
  }
});

test("equal-activity repository order aligns the initial target over names", () => {
  const contexts = [
    head("first", "repo-z", 50, { name: "Zebra", streamId: "stream-a" }),
    head("last", "repo-a", 50, { name: "Alpha", streamId: "stream-z" }),
  ];
  for (const heads of [contexts, [...contexts].reverse()]) {
    const top = projectGroups(heads, {}).at(0)?.streams.at(0)?.context;
    assert.equal(top?.id, "first");
    assert.equal(top, latestChangedContext(heads));
  }
});

test("stray stdin contexts group like any repository, by name and activity", () => {
  const groups = projectGroups(
    [
      head("repository", "api", 1),
      head("capture", "", 50, {
        source: "stdin",
        repositoryId: null,
        streamId: null,
        name: "Piped diff",
      }),
    ],
    null,
  );
  assert.deepEqual(
    groups.map((group) => [group.id, group.name]),
    [
      ["name:Piped diff", "Piped diff"],
      ["api", "api"],
    ],
  );
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

function observed(
  id: string,
  repositoryId: string,
  activity: number,
  {
    hostname = "laptop",
    worktree = "",
    sessions = [],
  }: {
    hostname?: string;
    worktree?: string;
    sessions?: { harness: string; name?: string }[];
  } = {},
): ApiContext {
  return head(id, repositoryId, activity, {
    worktreeName: worktree || null,
    observation: {
      sourceId: `source-${hostname}`,
      hostname,
      runId: "",
      agent: "",
      remoteUrl: "",
      head: null,
      repositoryKey: repositoryId,
      repositoryName: repositoryId,
      checkoutKey: id,
      linkedWorktree: worktree !== "",
      root: `/repos/${id}`,
      worktreeName: worktree,
      branch: `branch-${id}`,
      collectedAt: 1,
      collectorVersion: "test",
      trigger: "agent-hook",
    },
    sessions: sessions.map((session, index) => ({
      sourceId: `source-${hostname}`,
      id: `${id}-${index}`,
      firstObservedAt: 1,
      lastObservedAt: 1,
      ...session,
    })),
  });
}

test("recent streams interleave repositories by activity", () => {
  const groups = projectGroups(
    [
      head("api-old", "api", 10),
      head("web-new", "web", 30),
      head("api-new", "api", 20),
    ],
    null,
  );
  assert.deepEqual(
    groups.map((group) => group.id),
    ["web", "api"],
  );
  assert.deepEqual(
    recentStreams(groups).map(({ context, group }) => [context.id, group.id]),
    [
      ["web-new", "web"],
      ["api-new", "api"],
      ["api-old", "api"],
    ],
  );
});

test("recent stream activity ties keep stable stream identity order", () => {
  const groups = projectGroups(
    [head("b", "web", 5), head("a", "api", 5), head("c", "api", 5)],
    null,
  );
  assert.deepEqual(
    recentStreams(groups).map(({ context }) => context.id),
    ["a", "b", "c"],
  );
});

test("repository marks use word initials or the name's first letters", () => {
  assert.equal(repoMonogram("diffx"), "Di");
  assert.equal(repoMonogram("agent-skills"), "AS");
  assert.equal(repoMonogram("tokenInsights"), "TI");
  assert.equal(repoMonogram("x"), "X");
});

test("branch filter matches every word across branch, worktree, host and sessions", () => {
  const [group] = projectGroups(
    [
      observed("auth", "api", 3, {
        worktree: "wt-auth",
        sessions: [{ harness: "claude", name: "fix auth" }],
      }),
      observed("main", "api", 2, { hostname: "ci" }),
    ],
    null,
  );
  assert.ok(group);
  const ids = (query: string) =>
    group.streams
      .filter((stream) => streamMatches(stream, query))
      .map((stream) => stream.context.id);
  assert.deepEqual(ids(""), ["auth", "main"]);
  assert.deepEqual(ids("WT-AUTH"), ["auth"]);
  assert.deepEqual(ids("ci branch-main"), ["main"]);
  assert.deepEqual(ids("fix claude"), ["auth"]);
  assert.deepEqual(ids("auth ci"), []);
});
