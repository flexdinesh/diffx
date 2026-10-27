import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiCommitSummary, ApiContext } from "@diffx/api";
import {
  commitEmptyState,
  commitMatches,
  readSavedCommit,
  resolveCommit,
  savedCommit,
  stepCommit,
  type CommitView,
} from "../src/commit-view.ts";
import { branchChanges } from "../src/diff-mode.ts";
import { diffComparison } from "../src/diff-comparison.ts";

function commit(
  oid: string,
  subject: string,
  options: Partial<ApiCommitSummary> = {},
  patchId: string | null = `patch-${oid}`,
): ApiCommitSummary {
  return {
    oid: oid.padEnd(40, "0"),
    parents: ["f".repeat(40)],
    ...(patchId !== null && { patchId }),
    subject,
    body: "",
    authorName: "Ada Lovelace",
    authoredAt: 1,
    committedAt: 1,
    merge: false,
    fileCount: 1,
    additions: 1,
    deletions: 0,
    ...options,
  };
}

const first = commit("a1", "Add parser");
const merge = commit(
  "b2",
  "Merge main",
  { parents: ["1".repeat(40), "2".repeat(40)], merge: true, fileCount: 0 },
  null,
);
const last = commit("c3", "Wire parser into CLI");
const commits = [first, merge, last];

test("an explicit commit resolves only to itself", () => {
  assert.deepEqual(
    resolveCommit(commits, { kind: "explicit", oid: last.oid }),
    { commit: last, notice: "" },
  );
  const rebased = { ...last, oid: "d4".padEnd(40, "0") };
  assert.deepEqual(
    resolveCommit([first, merge, rebased], {
      kind: "explicit",
      oid: last.oid,
    }),
    { commit: null, notice: "" },
  );
});

test("a remembered commit follows a rebase by patch ID", () => {
  const rebased = { ...last, oid: "d4".padEnd(40, "0") };
  assert.deepEqual(
    resolveCommit([first, merge, rebased], {
      kind: "saved",
      oid: last.oid,
      patchId: last.patchId ?? null,
    }),
    { commit: rebased, notice: "" },
  );
});

test("a remembered commit that left the branch restarts at the oldest", () => {
  const result = resolveCommit([first, last], {
    kind: "saved",
    oid: merge.oid,
    patchId: null,
  });
  assert.equal(result.commit, first);
  assert.match(result.notice, /^Commit b200000 is not in this snapshot/);
  assert.deepEqual(
    resolveCommit([], { kind: "saved", oid: merge.oid, patchId: null }),
    { commit: null, notice: "" },
  );
});

test("stepping stops at either end and skips nothing, including merges", () => {
  assert.equal(stepCommit(commits, first.oid, -1), null);
  assert.equal(stepCommit(commits, first.oid, 1), merge);
  assert.equal(stepCommit(commits, merge.oid, 1), last);
  assert.equal(stepCommit(commits, last.oid, 1), null);
  assert.equal(stepCommit(commits, "missing", 1), null);
});

test("the filter matches every word against subject, author or SHA prefix", () => {
  assert.equal(commitMatches(last, "parser cli"), true);
  assert.equal(commitMatches(last, "ADA"), true);
  assert.equal(commitMatches(last, "c3"), true);
  assert.equal(commitMatches(last, "3c"), false);
  assert.equal(commitMatches(last, "parser merge"), false);
});

test("saved commits round-trip and reject malformed storage", () => {
  assert.deepEqual(readSavedCommit(savedCommit(first)), {
    oid: first.oid,
    patchId: "patch-a1",
  });
  assert.deepEqual(readSavedCommit(savedCommit(merge)), {
    oid: merge.oid,
    patchId: null,
  });
  for (const raw of [null, "", "{", "[]", '{"oid":""}', '{"oid":1}'])
    assert.equal(readSavedCommit(raw), null);
});

test("commit views without a diff explain why", () => {
  const view: CommitView = {
    oid: merge.oid,
    summary: merge,
    status: "ready",
    detail: "",
    commits,
    omitted: 0,
    notice: "",
  };
  assert.equal(commitEmptyState(view)?.title, "Merge commit");
  assert.equal(commitEmptyState({ ...view, summary: first }), null);
  assert.equal(
    commitEmptyState({ ...view, summary: null, status: "unavailable" })?.title,
    "Commit not in this snapshot",
  );
  assert.deepEqual(
    commitEmptyState({
      ...view,
      summary: null,
      status: "error",
      detail: "Server unavailable",
    }),
    { title: "Cannot load commits", description: "Server unavailable" },
  );
  assert.equal(
    commitEmptyState({ ...view, summary: null, status: "loading" }),
    null,
  );
});

const context: ApiContext = {
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
  commits: { count: 3, omitted: 0 },
  capabilities: {
    diff: {
      scopes: {
        state: "enabled",
        values: ["all", "uncommitted", "committed"],
      },
      refresh: { state: "unavailable" },
      stagingMetadata: { state: "enabled" },
    },
    files: { contents: { state: "enabled" } },
    review: { comments: { state: "enabled" } },
  },
  scopeSummaries: [
    { scope: "all", comparisonKind: "combined", fileCount: 1 },
    { scope: "uncommitted", comparisonKind: "uncommitted", fileCount: 0 },
    { scope: "committed", comparisonKind: "committed", fileCount: 1 },
  ],
};

test("commit views link back to committed branch changes, else combined", () => {
  assert.deepEqual(branchChanges(context), {
    scope: "committed",
    kind: "committed",
  });
  const combinedOnly: ApiContext = {
    ...context,
    capabilities: {
      ...context.capabilities,
      diff: {
        ...context.capabilities.diff,
        scopes: { state: "enabled", values: ["all"] },
      },
    },
  };
  assert.deepEqual(branchChanges(combinedOnly), {
    scope: "all",
    kind: "combined",
  });
});

test("a commit compares its first parent with itself", () => {
  const comparison = diffComparison(context, "committed", null, "commit", last);
  assert.equal(comparison.summary, "c300000^ → c300000");
  assert.deepEqual(comparison.details.slice(0, 2), [
    { label: "Commit", value: last.oid },
    { label: "Parent", value: "f".repeat(40) },
  ]);
  const root = diffComparison(context, "committed", null, "commit", {
    ...first,
    parents: [],
  });
  assert.equal(root.summary, "Empty tree → a100000");
});
