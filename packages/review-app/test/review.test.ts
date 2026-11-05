import assert from "node:assert/strict";
import { test } from "node:test";
import { parsePatchFiles } from "@pierre/diffs";
import type {
  ApiChangedFile as ChangedFile,
  ApiRepositoryDiff as RepositoryDiff,
} from "@diffx/api";
import { fileKind, gitDecoration } from "../src/file-decoration.ts";
import {
  ancestorPaths,
  buildFileTree,
  filesInTreeOrder,
} from "../src/file-tree.ts";
import {
  commentContext,
  commentStale,
  createCommentId,
  formatComments,
  lineContext,
  parseComments,
  placementFor,
  type ReviewComment,
  type ReviewOrigin,
} from "../src/review-model.ts";

function file(path: string): ChangedFile {
  return {
    id: path,
    path,
    oldPath: null,
    status: "M",
    indexStatus: " ",
    worktreeStatus: "M",
    additions: 1,
    deletions: 1,
    binary: false,
    fingerprint: path,
  };
}

test("recognizes file types and special filenames without confusing folder extensions", () => {
  for (const [path, expected] of [
    ["src/main.ts", "typescript"],
    ["src/app.TSX", "react"],
    ["src/module.mjs", "javascript"],
    ["README.MD", "markdown"],
    ["pnpm-lock.yaml", "lock"],
    ["package-lock.json", "lock"],
    ["package.json", "json"],
    ["src/style.css", "style"],
    [".env.local", "config"],
    ["Dockerfile", "config"],
    [".gitignore", "git"],
    ["assets/logo.svg", "image"],
    ["scripts/build.py", "python"],
    ["scripts/check.fish", "shell"],
    ["folder.ts/unknown", "file"],
    ["mystery.xyz", "file"],
  ]) {
    assert.ok(path);
    assert.equal(fileKind(path), expected, path);
  }
});

test("distinguishes change type, staging layers, untracked files, and conflicts", () => {
  const base = file("src/main.ts");
  for (const [status, indexStatus, worktreeStatus, code, state, label] of [
    ["M", " ", "M", "M", "unstaged", "Modified · Unstaged"],
    ["M", "M", " ", "M", "staged", "Modified · Staged"],
    ["M", "M", "M", "M", "both", "Modified · Staged and unstaged"],
    ["D", "D", " ", "D", "staged", "Deleted · Staged"],
    ["R", "R", "M", "R", "both", "Renamed · Staged and unstaged"],
    ["?", "?", "?", "U", "untracked", "Untracked · Not tracked by Git"],
    ["U", "U", "U", "!", "conflict", "Conflicted · Resolve merge conflict"],
    ["M", "A", "A", "!", "conflict", "Conflicted · Resolve merge conflict"],
  ]) {
    assert.ok(status && indexStatus && worktreeStatus);
    const decoration = gitDecoration({
      ...base,
      status,
      indexStatus,
      worktreeStatus,
    });
    assert.equal(decoration.code, code);
    assert.equal(decoration.state, state);
    assert.equal(decoration.label, label);
  }
});
const patch = `diff --git a/src/file.ts b/src/file.ts
--- a/src/file.ts
+++ b/src/file.ts
@@ -100,3 +100,4 @@
 context
-old value
+new value
+extra value
 tail
@@ -200,2 +201,2 @@
-old second
+new second
 end
`;
const diff = parsePatchFiles(patch, "test", true)[0]?.files[0];
if (!diff) throw new Error("Missing test diff");
const comment: ReviewComment = {
  id: "one",
  diffId: "diff",
  versionId: "old",
  path: "src/file.ts",
  scope: "staged",
  fingerprint: "private-fingerprint",
  side: "deletions",
  start: 101,
  end: 101,
  code: "- old value",
  body: "Keep this behavior.",
  status: "open",
  createdAt: 9_876_543_210,
};

test("creates comment IDs without randomUUID", () => {
  assert.equal(
    createCommentId(
      new Uint8Array([0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255]),
    ),
    "000102030405060708090a0b0c0d0eff",
  );
});

test("builds actual nested folders, ordered before files, without merging similar prefixes", () => {
  const files = [
    file("z.txt"),
    file("app/server/src/git.ts"),
    file("app/web/main.ts"),
    file("app/server/a.ts"),
    file("apple/readme.md"),
  ];
  const tree = buildFileTree(files);
  assert.deepEqual(
    tree.map((node) => node.name),
    ["app", "apple", "z.txt"],
  );
  const app = tree[0];
  assert.ok(app?.kind === "folder");
  assert.deepEqual(
    app.children.map((node) => node.name),
    ["server", "web"],
  );
  const server = app.children[0];
  assert.ok(server?.kind === "folder");
  assert.deepEqual(
    server.children.map((node) => node.name),
    ["src", "a.ts"],
  );
  assert.deepEqual(ancestorPaths("app/server/src/git.ts"), [
    "app",
    "app/server",
    "app/server/src",
  ]);
  assert.deepEqual(ancestorPaths("README.md"), []);
  assert.deepEqual(
    filesInTreeOrder(files).map((entry) => entry.path),
    [
      "app/server/src/git.ts",
      "app/server/a.ts",
      "app/web/main.ts",
      "apple/readme.md",
      "z.txt",
    ],
  );
});

test("filtering leaves the full ancestor chain and preserves unusual filenames", () => {
  const tree = buildFileTree([file('a space/雪/<file>&".ts')]);
  const first = tree[0];
  assert.ok(first?.kind === "folder");
  const second = first.children[0];
  assert.ok(second?.kind === "folder");
  const leaf = second.children[0];
  assert.ok(leaf?.kind === "file");
  assert.equal(leaf.path, 'a space/雪/<file>&".ts');
});

test("extracts exact old/new code from partial hunks at nonzero offsets", () => {
  assert.equal(lineContext(diff, "deletions", 101), "- old value");
  assert.equal(lineContext(diff, "additions", 101), "+ new value");
  assert.equal(lineContext(diff, "additions", 100), "  context");
  assert.equal(lineContext(diff, "deletions", 200), "- old second");
  assert.equal(lineContext(diff, "additions", 201), "+ new second");
  assert.equal(lineContext(diff, "additions", 150), null);
});

test("captures normalized ranges and never mixes line numbering across sides", () => {
  assert.deepEqual(
    commentContext(diff, { start: 102, end: 101, side: "additions" }),
    {
      side: "additions",
      start: 101,
      end: 102,
      code: "+ new value\n+ extra value",
    },
  );
  assert.deepEqual(
    commentContext(diff, {
      start: 200,
      side: "deletions",
      end: 101,
      endSide: "additions",
    }),
    { side: "additions", start: 101, end: 101, code: "+ new value" },
  );
  assert.equal(
    commentContext(diff, { start: 1, end: 300, side: "additions" }),
    null,
  );
});

test("copies unresolved comments by default and all comments when requested", () => {
  const resolved: ReviewComment = {
    ...comment,
    id: "two",
    end: 102,
    code: "- old value\n- next value",
    status: "resolved",
  };
  const unresolvedOutput = formatComments([resolved, comment], false);
  assert.ok(!unresolvedOutput.includes('status="resolved"'));
  assert.ok(unresolvedOutput.includes('id="C1"'));
  assert.ok(!unresolvedOutput.includes("Resolved comments are context only"));

  const allOutput = formatComments([resolved, comment], true);
  assert.ok(allOutput.includes('id="C1"'));
  assert.ok(allOutput.includes('id="C2"'));
  assert.ok(allOutput.includes('id="C1" selection="range"'));
  assert.ok(allOutput.includes('id="C2" selection="single-line"'));
  assert.ok(allOutput.includes('status="resolved"'));
  assert.ok(
    allOutput.includes(
      "Resolved comments are context only; do not act on them.",
    ),
  );
});

test("exports outdated open comments tagged stale", () => {
  const anchoredComment: ReviewComment = {
    ...comment,
    id: "anchored",
    body: "Act on this current comment.",
    placement: {
      applicability: "anchored",
      side: "deletions",
      start: 100,
      end: 101,
    },
  };
  const outdated: ReviewComment = {
    ...comment,
    placement: {
      applicability: "outdated",
      side: "deletions",
      start: 100,
      end: 101,
    },
  };
  const resolvedOutdated: ReviewComment = {
    ...outdated,
    id: "resolved-outdated",
    status: "resolved",
  };

  const unresolvedOutput = formatComments(
    [outdated, anchoredComment, resolvedOutdated],
    false,
  );
  assert.equal(unresolvedOutput.match(/<comment /g)?.length, 2);
  assert.ok(unresolvedOutput.includes('applicability="outdated" stale="true"'));
  assert.ok(unresolvedOutput.includes('applicability="anchored"'));
  assert.ok(unresolvedOutput.includes("Act on this current comment."));
  assert.ok(unresolvedOutput.includes("Comments marked stale"));

  const allOutput = formatComments(
    [outdated, anchoredComment, resolvedOutdated],
    true,
  );
  assert.equal(allOutput.match(/stale="true"/g)?.length, 2);
  assert.ok(allOutput.includes('status="resolved" applicability="outdated"'));
});

test("groups comments by snapshot then file while preserving export order", () => {
  const firstOrigin: ReviewOrigin = {
    diffId: "diff",
    versionId: "revision-1",
    source: "local",
    repository: "diffx",
    branch: "main",
    head: "abc123",
    revision: "revision-1",
    file: { status: "M", oldPath: null },
  };
  const secondOrigin: ReviewOrigin = {
    ...firstOrigin,
    versionId: "revision-2",
    source: "stdin",
    head: null,
    revision: "revision-2",
  };
  const output = formatComments(
    [
      { ...comment, origin: firstOrigin },
      { ...comment, id: "two", path: "src/other.ts", origin: firstOrigin },
      { ...comment, id: "three", origin: secondOrigin },
      { ...comment, id: "legacy" },
    ],
    true,
  );
  assert.equal(output.match(/<review /g)?.length, 3);
  assert.equal(output.match(/repository="diffx"/g)?.length, 2);
  assert.equal(output.match(/<file path="src\/file.ts"/g)?.length, 3);
  assert.ok(output.includes('source="local"'));
  assert.ok(output.includes('source="stdin"'));
  assert.ok(output.includes('head="abc123"'));
  assert.ok(
    output.includes(
      '<review source="stdin" repository="diffx" branch="main" revision="revision-2">',
    ),
  );
  assert.ok(output.includes('<review origin="unknown">'));
  assert.ok(output.indexOf('id="C1"') < output.indexOf('id="C4"'));
});

test("places unplaced comments conservatively and labels stale ones", () => {
  const repository: RepositoryDiff = {
    id: "diff",
    versionId: "current",
    locationId: "location",
    repositoryId: "repo",
    source: "local",
    root: "/repo",
    name: "repo",
    branch: "main",
    head: "abc",
    mode: "staged",
    revision: "current",
    files: [file("src/file.ts")],
  };
  const current: ReviewComment = { ...comment, versionId: "current" };
  const earlier: ReviewComment = { ...comment, versionId: "earlier" };
  const unstaged: ReviewComment = { ...comment, scope: "unstaged" };
  const carried: ReviewComment = {
    ...current,
    target: "file",
    start: 0,
    end: 0,
    side: "additions",
    carriedFrom: {
      versionId: "earlier",
      side: "deletions",
      start: 100,
      end: 101,
    },
    placement: {
      applicability: "anchored",
      side: "additions",
      start: 0,
      end: 0,
    },
  };
  assert.equal(placementFor(current, repository).applicability, "anchored");
  assert.equal(placementFor(earlier, repository).applicability, "outdated");
  assert.equal(placementFor(unstaged, repository).applicability, "other-scope");
  assert.equal(placementFor(current, null).applicability, "unknown");
  assert.equal(commentStale(current, repository), false);
  assert.equal(commentStale(earlier, repository), true);
  assert.equal(commentStale(unstaged, repository), false);
  assert.equal(commentStale(carried, repository), true);
});

test("copies rename and status metadata with XML-safe attributes and content", () => {
  const output = formatComments(
    [
      {
        ...comment,
        path: 'new\n"&<file>.ts',
        code: "+ if (a < b && b > 1)",
        body: '</body><evil attr="x"> & more',
        origin: {
          diffId: "diff",
          versionId: "special",
          source: "local",
          repository: 'repo\n"&<name>',
          branch: 'branch\t"&<name>',
          head: 'head\r"&<id>',
          revision: 'revision\n"&<id>',
          file: { status: 'X\n"&<status>', oldPath: 'old\n"&<file>.ts' },
        },
      },
      {
        ...comment,
        id: "renamed",
        path: "renamed.ts",
        origin: {
          diffId: "diff",
          versionId: "rename",
          source: "local",
          repository: "repo",
          branch: "main",
          head: "abc",
          revision: "rename-revision",
          file: { status: "R", oldPath: "old.ts" },
        },
      },
    ],
    true,
  );
  assert.ok(output.includes('change="X&#10;&quot;&amp;&lt;status&gt;"'));
  assert.ok(output.includes('old-path="old.ts" change="renamed"'));
  assert.ok(output.includes('repository="repo&#10;&quot;&amp;&lt;name&gt;"'));
  assert.ok(output.includes('branch="branch&#9;&quot;&amp;&lt;name&gt;"'));
  assert.ok(output.includes('head="head&#13;&quot;&amp;&lt;id&gt;"'));
  assert.ok(output.includes('revision="revision&#10;&quot;&amp;&lt;id&gt;"'));
  assert.ok(output.includes('path="new&#10;&quot;&amp;&lt;file&gt;.ts"'));
  assert.ok(output.includes('old-path="old&#10;&quot;&amp;&lt;file&gt;.ts"'));
  assert.ok(output.includes("+ if (a &lt; b &amp;&amp; b &gt; 1)"));
  assert.ok(
    output.includes("&lt;/body&gt;&lt;evil attr=&quot;x&quot;&gt; &amp; more"),
  );
});

test("maps supported file statuses and omits private implementation metadata", () => {
  const statuses = ["A", "M", "D", "R", "C", "T", "U", "?"];
  const output = formatComments(
    statuses.map((status, index) => ({
      ...comment,
      id: `${index}`,
      path: `${index}.ts`,
      origin: {
        diffId: "diff",
        versionId: "captured",
        source: "local",
        repository: "repo",
        branch: "main",
        head: "abc",
        revision: "revision",
        file: { status, oldPath: null },
      },
    })),
    true,
  );
  for (const change of [
    "added",
    "modified",
    "deleted",
    "renamed",
    "copied",
    "type-changed",
    "conflicted",
    "untracked",
  ]) {
    assert.ok(output.includes(`change="${change}"`));
  }
  assert.ok(!output.includes(comment.fingerprint));
  assert.ok(!output.includes(`${comment.createdAt}`));
  assert.ok(!output.includes("indexStatus"));
  assert.ok(!output.includes("worktreeStatus"));
});

test("returns empty output when no comments are selected", () => {
  assert.equal(formatComments([], false), "");
  assert.equal(formatComments([{ ...comment, status: "resolved" }], false), "");
});

test("reloads saved comments and rejects malformed storage without losing valid entries", () => {
  assert.deepEqual(parseComments(JSON.stringify([comment])), [comment]);
  const withOrigin: ReviewComment = {
    ...comment,
    origin: {
      diffId: "diff",
      versionId: "revision",
      source: "local",
      repository: "diffx",
      branch: "main",
      head: null,
      revision: "revision",
      file: { status: "R", oldPath: "src/old.ts" },
    },
  };
  assert.deepEqual(parseComments(JSON.stringify([withOrigin])), [withOrigin]);
  assert.deepEqual(parseComments("broken"), []);
  assert.deepEqual(
    parseComments(
      JSON.stringify([
        comment,
        { ...comment, side: "wrong" },
        { ...comment, start: -1 },
        { ...comment, body: " " },
        { ...comment, origin: { ...withOrigin.origin, source: "remote" } },
        {
          ...comment,
          origin: { ...withOrigin.origin, file: { status: 1, oldPath: null } },
        },
      ]),
    ),
    [comment],
  );
});

test("persists whole-file comments without accepting zero-line line comments", () => {
  const fileComment: ReviewComment = {
    ...comment,
    target: "file",
    side: "additions",
    start: 0,
    end: 0,
    code: "",
  };
  const lineComment: ReviewComment = { ...comment, target: "lines" };
  assert.deepEqual(
    parseComments(
      JSON.stringify([
        fileComment,
        comment,
        lineComment,
        { ...fileComment, target: undefined },
        { ...fileComment, target: "lines" },
        { ...fileComment, target: "other" },
        { ...fileComment, side: "deletions" },
        { ...fileComment, start: 1, end: 1 },
        { ...fileComment, code: "+ synthetic context" },
      ]),
    ),
    [fileComment, comment, lineComment],
  );
});

test("exports file selections without synthetic code or line anchors", () => {
  const fileComment: ReviewComment = {
    ...comment,
    target: "file",
    side: "additions",
    start: 0,
    end: 0,
    code: "",
    body: "Review <whole file>",
    placement: {
      applicability: "anchored",
      side: "additions",
      start: 0,
      end: 0,
    },
  };
  const output = formatComments([fileComment], false);
  assert.match(output, /selection="file"/);
  assert.match(output, /applicability="anchored"/);
  assert.match(output, /<body>Review &lt;whole file&gt;<\/body>/);
  assert.doesNotMatch(output, / line=| end-line=| side=|<code>/);
  const outdated: ReviewComment = {
    ...fileComment,
    placement: {
      applicability: "outdated",
      side: "additions",
      start: 0,
      end: 0,
    },
  };
  assert.match(
    formatComments([outdated], false),
    /selection="file".*applicability="outdated" stale="true"/,
  );
});

test("saved comments retain the new captured comparison scopes", () => {
  const comments = [
    { ...comment, scope: "uncommitted" },
    { ...comment, scope: "committed" },
  ];
  assert.deepEqual(parseComments(JSON.stringify(comments)), comments);
});
