import assert from "node:assert/strict";
import { test } from "node:test";
import { parsePatchFiles, type CodeViewItem } from "@pierre/diffs";
import {
  changeStops,
  stepChangeStop,
  type ChangeStop,
  type StopRect,
} from "../src/hunk-navigation.ts";

function diffItem(path: string, patch: string): CodeViewItem<undefined> {
  const [fileDiff] = parsePatchFiles(patch, path).flatMap(
    (parsed) => parsed.files,
  );
  assert.ok(fileDiff);
  return { id: path, type: "diff", fileDiff };
}

// One hunk holding a replacement, then an addition after two context lines.
const twoBlocks = `diff --git a/a.ts b/a.ts
--- a/a.ts
+++ b/a.ts
@@ -10,7 +10,8 @@ function a() {
 one
 two
 three
-four
+FOUR
 five
 six
+six-and-a-half
 seven
`;
// Two hunks: an addition at the top, a deletion further down.
const twoHunks = `diff --git a/b.ts b/b.ts
--- a/b.ts
+++ b/b.ts
@@ -1,2 +1,3 @@
+import x;
 alpha
 beta
@@ -40,3 +41,2 @@
 gamma
-delta
 epsilon
`;
const added = `diff --git a/c.ts b/c.ts
new file mode 100644
--- /dev/null
+++ b/c.ts
@@ -0,0 +1,2 @@
+first
+second
`;

const items = new Map([
  ["a.ts", diffItem("a.ts", twoBlocks)],
  ["b.ts", diffItem("b.ts", twoHunks)],
  ["c.ts", diffItem("c.ts", added)],
  [
    "binary.png",
    {
      id: "binary.png",
      type: "file",
      file: { name: "binary.png", contents: "Binary file\n", lang: "text" },
    } satisfies CodeViewItem<undefined>,
  ],
]);

test("change stops split hunks into contiguous change blocks", () => {
  const stops = changeStops(["a.ts"], items, new Set());
  assert.deepEqual(stops, [
    {
      path: "a.ts",
      index: 0,
      count: 2,
      deletions: { start: 13, end: 13 },
      additions: { start: 13, end: 13 },
      anchor: { side: "additions", lineNumber: 10 },
    },
    {
      path: "a.ts",
      index: 1,
      count: 2,
      deletions: null,
      additions: { start: 16, end: 16 },
      anchor: { side: "additions", lineNumber: 14 },
    },
  ]);
});

test("change stops anchor at preceding context, else at the block", () => {
  const stops = changeStops(["b.ts", "c.ts"], items, new Set());
  assert.deepEqual(
    stops.map(({ path, deletions, additions, anchor }) => ({
      path,
      deletions,
      additions,
      anchor,
    })),
    [
      {
        path: "b.ts",
        deletions: null,
        additions: { start: 1, end: 1 },
        anchor: { side: "additions", lineNumber: 1 },
      },
      {
        path: "b.ts",
        deletions: { start: 41, end: 41 },
        additions: null,
        anchor: { side: "additions", lineNumber: 41 },
      },
      {
        path: "c.ts",
        deletions: null,
        additions: { start: 1, end: 2 },
        anchor: { side: "additions", lineNumber: 1 },
      },
    ],
  );
});

test("change stops follow viewer order and skip collapsed and placeholder files", () => {
  const stops = changeStops(
    ["c.ts", "binary.png", "missing.ts", "a.ts", "b.ts"],
    items,
    new Set(["a.ts"]),
  );
  assert.deepEqual(
    stops.map((stop) => `${stop.path}#${stop.index}`),
    ["c.ts#0", "b.ts#0", "b.ts#1"],
  );
});

// Stops laid out 100px apart from the given top.
function layout(stops: readonly ChangeStop[], start = 0) {
  const rects = new Map<ChangeStop, StopRect>();
  stops.forEach((stop, position) =>
    rects.set(stop, {
      top: start + position * 100,
      bottom: start + position * 100 + 40,
    }),
  );
  return (stop: ChangeStop) => {
    const rect = rects.get(stop);
    assert.ok(rect);
    return rect;
  };
}

const all = changeStops(["a.ts", "b.ts", "c.ts"], items, new Set());
const rect = layout(all);
const key = (stop: ChangeStop | null) =>
  stop ? `${stop.path}#${stop.index}` : null;

test("without a current stop, the viewport top picks the next or previous", () => {
  const viewport = { top: 150, bottom: 450 };
  assert.equal(key(stepChangeStop(all, 1, viewport, rect, null)), "b.ts#0");
  assert.equal(key(stepChangeStop(all, -1, viewport, rect, null)), "a.ts#1");
});

test("the first stop below the top counts as next even when already visible", () => {
  const viewport = { top: 0, bottom: 300 };
  assert.equal(key(stepChangeStop(all, 1, viewport, rect, null)), "a.ts#0");
});

test("a visible current stop steps to its neighbor, crossing files", () => {
  // The last stops cannot reach the top: the viewport ends at the list end.
  const viewport = { top: 100, bottom: 500 };
  const cursor = { path: "a.ts", index: 1 };
  assert.equal(key(stepChangeStop(all, 1, viewport, rect, cursor)), "b.ts#0");
  assert.equal(
    key(stepChangeStop(all, 1, viewport, rect, { path: "b.ts", index: 1 })),
    "c.ts#0",
  );
  assert.equal(key(stepChangeStop(all, -1, viewport, rect, cursor)), "a.ts#0");
});

test("a current stop scrolled out of view yields to the viewport", () => {
  const viewport = { top: 250, bottom: 450 };
  const cursor = { path: "a.ts", index: 0 };
  assert.equal(key(stepChangeStop(all, 1, viewport, rect, cursor)), "b.ts#1");
});

test("a current stop that left the list yields to the viewport", () => {
  const viewport = { top: 150, bottom: 450 };
  const cursor = { path: "gone.ts", index: 0 };
  assert.equal(key(stepChangeStop(all, 1, viewport, rect, cursor)), "b.ts#0");
});

test("stepping stops at either end without wrapping", () => {
  const viewport = { top: 0, bottom: 600 };
  assert.equal(
    stepChangeStop(all, 1, viewport, rect, { path: "c.ts", index: 0 }),
    null,
  );
  assert.equal(
    stepChangeStop(all, -1, viewport, rect, { path: "a.ts", index: 0 }),
    null,
  );
  assert.equal(stepChangeStop(all, -1, viewport, rect, null), null);
  assert.equal(
    stepChangeStop(all, 1, { top: 900, bottom: 1200 }, rect, null),
    null,
  );
  assert.equal(stepChangeStop([], 1, viewport, rect, null), null);
});
