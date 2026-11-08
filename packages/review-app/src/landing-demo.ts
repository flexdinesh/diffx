import { formatComments, type ReviewComment } from "./review-comments.ts";

/**
 * The landing page's demonstration review: a synthetic change, one review
 * comment on it, and the agent instructions diffx exports for that comment.
 * Nothing here comes from a real repository.
 */
export const demoPath = "src/retry.ts";

// Built line by line: unified diff context lines start with a space, which a
// template literal's blank lines would lose to formatting.
export const demoPatch = [
  "diff --git a/src/retry.ts b/src/retry.ts",
  "index 3f2a9c1..8d41e07 100644",
  "--- a/src/retry.ts",
  "+++ b/src/retry.ts",
  "@@ -5,7 +5,14 @@ const baseDelay = 250;",
  " export async function retry<T>(task: () => Promise<T>, attempts = 6) {",
  "-  return task();",
  "+  for (let attempt = 0; ; attempt++) {",
  "+    try {",
  "+      return await task();",
  "+    } catch (error) {",
  "+      if (attempt + 1 >= attempts) throw error;",
  "+      await sleep(baseDelay * 2 ** attempt);",
  "+    }",
  "+  }",
  " }",
  " ",
  " export function isRetryable(status: number) {",
  "   return status === 429 || status >= 500;",
  " }",
  "",
].join("\n");

/** The commented addition line, numbered in the new file. */
export const demoCommentLine = 11;

export const demoComment: ReviewComment = {
  id: "demo-comment",
  diffId: "demo",
  versionId: "demo",
  path: demoPath,
  scope: "uncommitted",
  fingerprint: "demo",
  side: "additions",
  start: demoCommentLine,
  end: demoCommentLine,
  code: "+       await sleep(baseDelay * 2 ** attempt);",
  body: "Cap this delay. Six attempts already wait about eight seconds, and callers that raise attempts wait minutes. Clamp it to 5 seconds and add jitter.",
  status: "open",
  createdAt: 0,
  origin: {
    diffId: "demo",
    versionId: "demo",
    source: "local",
    repository: "acme/api",
    branch: "retry-backoff",
    head: null,
    revision: "8d41e07",
    file: { status: "M", oldPath: null },
  },
  placement: {
    applicability: "anchored",
    side: "additions",
    start: demoCommentLine,
    end: demoCommentLine,
  },
};

/** Exactly what "Copy review" would hand an agent for the demo comment. */
export const demoAgentReview = formatComments([demoComment], false);

/** The demo window's synthetic checkout: its branch and changed files. */
export const demoRepository = { name: "acme/api", branch: "retry-backoff" };

export type DemoTreeEntry = {
  name: string;
  depth: number;
  kind: "directory" | "file";
  open?: boolean;
  status?: "M" | "A";
  selected?: boolean;
};

export const demoTree: readonly DemoTreeEntry[] = [
  { name: "src", depth: 0, kind: "directory", open: true },
  { name: "retry.ts", depth: 1, kind: "file", status: "M", selected: true },
  { name: "http.ts", depth: 1, kind: "file", status: "M" },
  { name: "retry.test.ts", depth: 1, kind: "file", status: "A" },
  { name: "docs", depth: 0, kind: "directory", open: false },
  { name: "CHANGELOG.md", depth: 0, kind: "file", status: "M" },
];

/** Totals for every file in the demo tree, not only the rendered patch. */
export const demoSummary = { files: 4, additions: 41, deletions: 6 };
