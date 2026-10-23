import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiContext } from "@diffx/api";
import {
  comparisonKind,
  defaultScope,
  emptyComparison,
  selectedScope,
} from "../src/diff-mode.ts";
import { diffComparison } from "../src/diff-comparison.ts";
import { gitDecoration } from "../src/file-decoration.ts";
import { sameRoute } from "../src/review-route.ts";

const captured: ApiContext = {
  id: "snapshot",
  streamId: "stream",
  latestContextId: null,
  kind: "observation",
  source: "local",
  name: "repo",
  branch: "feature",
  worktreeName: null,
  root: "/repo",
  repositoryId: "repo",
  locationId: "tree",
  createdAt: 1,
  lastSubmittedAt: 1,
  lastChangedAt: 1,
  changedFileCount: 1,
  expiresAt: null,
  submittedFrom: null,
  availability: "available",
  capabilities: {
    diff: {
      scopes: {
        state: "enabled",
        values: ["all", "uncommitted", "staged", "unstaged", "committed"],
      },
      refresh: { state: "unavailable" },
      stagingMetadata: { state: "enabled" },
    },
    files: { contents: { state: "enabled" } },
    review: { comments: { state: "enabled" } },
  },
  scopeSummaries: [
    { scope: "all", comparisonKind: "combined", fileCount: 0 },
    { scope: "uncommitted", comparisonKind: "uncommitted", fileCount: 0 },
    { scope: "staged", comparisonKind: "staged", fileCount: 1 },
    { scope: "unstaged", comparisonKind: "unstaged", fileCount: 1 },
    { scope: "committed", comparisonKind: "committed", fileCount: 1 },
  ],
};

test("defaults use captured staging counts even when net changes cancel", () => {
  assert.equal(defaultScope(captured), "uncommitted");
  const clean = {
    ...captured,
    scopeSummaries: (captured.scopeSummaries ?? []).map((summary) => ({
      ...summary,
      fileCount: 0,
    })),
  };
  assert.equal(defaultScope(clean), "committed");
  assert.equal(selectedScope(captured, "all", "staged").mode, "all");
  assert.equal(selectedScope(captured, null, "staged").mode, "staged");
  assert.equal(selectedScope(captured, null, "missing").mode, "uncommitted");
  assert.deepEqual(selectedScope(captured, "missing", "staged"), {
    mode: "uncommitted",
    requestedScope: "missing",
    scopeAvailable: false,
  });
});

test("legacy all gets captured semantics without conflating storage scope", () => {
  const kinds: ("uncommitted" | "committed" | "patch")[] = [
    "uncommitted",
    "committed",
    "patch",
  ];
  for (const kind of kinds) {
    const context: ApiContext = {
      ...captured,
      capabilities: {
        ...captured.capabilities,
        diff: {
          ...captured.capabilities.diff,
          scopes: { state: "enabled", values: ["all"] },
        },
      },
      scopeSummaries: [{ scope: "all", comparisonKind: kind, fileCount: 1 }],
    };
    assert.equal(comparisonKind(context, "all"), kind);
    assert.equal(defaultScope(context), "all");
    assert.equal(
      selectedScope(context, "committed", null).scopeAvailable,
      false,
    );
  }
});

test("empty net comparisons direct readers to captured edits", () => {
  assert.equal(
    emptyComparison(captured, "uncommitted").title,
    "No net uncommitted changes",
  );
  assert.match(
    emptyComparison(captured, "uncommitted").description,
    /Staged, Unstaged/,
  );
  assert.equal(
    emptyComparison(captured, "all").title,
    "No net combined changes",
  );
  assert.match(emptyComparison(captured, "all").description, /Branch changes/);
});

test("committed baseline uses captured merge base even with staging capability", () => {
  const context: ApiContext = {
    ...captured,
    observation: {
      sourceId: "machine",
      hostname: "host",
      runId: "",
      agent: "",
      trigger: "manual",
      repositoryKey: "repo",
      repositoryName: "repo",
      remoteUrl: "",
      checkoutKey: "tree",
      root: "/repo",
      worktreeName: "repo",
      branch: "feature",
      head: "captured-head",
      collectedAt: 1,
      collectorVersion: "test",
      comparison: {
        kind: "branch",
        baseRef: "refs/heads/main",
        baseCommit: "base",
        mergeBase: "merge-base",
      },
    },
  };
  const committed = diffComparison(context, "committed", "unrelated-head");
  assert.match(committed.summary, /→ HEAD$/);
  assert.match(
    committed.description,
    /Index and working tree changes are excluded/,
  );
  assert.ok(
    committed.details.some(
      ({ label, value }) => label === "HEAD" && value === "captured-head",
    ),
  );
  assert.match(diffComparison(context, "all", null).summary, /→ working tree$/);
  assert.match(diffComparison(context, "uncommitted", null).summary, /^HEAD/);
});

test("committed file labels never imply staging even if statuses are present", () => {
  const decoration = gitDecoration(
    {
      id: "file",
      path: "file.ts",
      oldPath: null,
      status: "M",
      indexStatus: "M",
      worktreeStatus: "M",
      additions: 1,
      deletions: 1,
      binary: false,
      fingerprint: "v1",
    },
    false,
  );
  assert.deepEqual(decoration, {
    code: "M",
    state: "change",
    label: "Modified",
    details: "Modified",
  });
});

test("route equality distinguishes scope navigation from another stream or snapshot", () => {
  assert.equal(
    sameRoute({ kind: "live", stream: "one" }, { kind: "live", stream: "one" }),
    true,
  );
  assert.equal(
    sameRoute({ kind: "live", stream: "one" }, { kind: "live", stream: "two" }),
    false,
  );
  assert.equal(
    sameRoute({ kind: "pinned", id: "one" }, { kind: "pinned", id: "two" }),
    false,
  );
});

test("missing baseline metadata stays unknown for committed captures", () => {
  const comparison = diffComparison(captured, "committed", null);
  assert.match(comparison.summary, /Merge base.*Not recorded.*→ HEAD$/);
  assert.match(
    comparison.description,
    /Index and working tree changes are excluded/,
  );
});
