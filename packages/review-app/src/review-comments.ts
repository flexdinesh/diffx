import type {
  AnnotationSide,
  FileDiffMetadata,
  SelectedLineRange,
} from "@pierre/diffs";
import { isDiffMode as diffMode } from "./diff-mode.ts";

import type { ApiReviewComment, ApiReviewMark } from "@diffx/api";
export type ReviewComment = ApiReviewComment;
export type ReviewOrigin = NonNullable<ReviewComment["origin"]>;
type CommentPlacement = NonNullable<ReviewComment["placement"]>;

export function lineContext(
  diff: FileDiffMetadata,
  side: AnnotationSide,
  lineNumber: number,
): string | null {
  const lines = side === "additions" ? diff.additionLines : diff.deletionLines;
  for (const hunk of diff.hunks) {
    const start =
      side === "additions" ? hunk.additionStart : hunk.deletionStart;
    const count =
      side === "additions" ? hunk.additionCount : hunk.deletionCount;
    if (lineNumber < start || lineNumber >= start + count) continue;
    const offset = lineNumber - start;
    const index = diff.isPartial
      ? (side === "additions"
          ? hunk.additionLineIndex
          : hunk.deletionLineIndex) + offset
      : lineNumber - 1;
    const content = lines[index];
    if (content === undefined) return null;
    let remaining = offset;
    let prefix = " ";
    for (const segment of hunk.hunkContent) {
      const length =
        segment.type === "context"
          ? segment.lines
          : side === "additions"
            ? segment.additions
            : segment.deletions;
      if (remaining < length) {
        prefix =
          segment.type === "context" ? " " : side === "additions" ? "+" : "-";
        break;
      }
      remaining -= length;
    }
    return `${prefix} ${content.replace(/\r?\n$/, "")}`;
  }
  const content = !diff.isPartial ? lines[lineNumber - 1] : undefined;
  return content === undefined ? null : `  ${content.replace(/\r?\n$/, "")}`;
}

export function commentContext(
  diff: FileDiffMetadata,
  range: SelectedLineRange,
) {
  const side = range.endSide ?? range.side;
  if (side !== "additions" && side !== "deletions") return null;
  const crossSide = range.side !== undefined && range.side !== side;
  const start = crossSide ? range.end : Math.min(range.start, range.end);
  const end = crossSide ? range.end : Math.max(range.start, range.end);
  if (start < 1 || end - start > 199) return null;
  const code: string[] = [];
  for (let line = start; line <= end; line++) {
    const content = lineContext(diff, side, line);
    if (content === null) return null;
    code.push(content);
  }
  return { side, start, end, code: code.join("\n") };
}

export type ReviewMark = ApiReviewMark;

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function validOrigin(value: unknown): value is ReviewOrigin {
  if (!record(value) || !record(value.file)) return false;
  return (
    typeof value.diffId === "string" &&
    typeof value.versionId === "string" &&
    (value.source === "local" || value.source === "stdin") &&
    typeof value.repository === "string" &&
    typeof value.branch === "string" &&
    (value.head === null || typeof value.head === "string") &&
    typeof value.revision === "string" &&
    typeof value.file.status === "string" &&
    (value.file.oldPath === null || typeof value.file.oldPath === "string")
  );
}

function validLines(side: unknown, start: unknown, end: unknown) {
  return (
    (side === "additions" || side === "deletions") &&
    typeof start === "number" &&
    Number.isInteger(start) &&
    start > 0 &&
    typeof end === "number" &&
    Number.isInteger(end) &&
    end >= start &&
    end - start < 200
  );
}

function validCarriedFrom(value: unknown) {
  return (
    record(value) &&
    typeof value.versionId === "string" &&
    validLines(value.side, value.start, value.end)
  );
}

function validAnchor(value: unknown) {
  return (
    record(value) &&
    typeof value.before === "string" &&
    typeof value.after === "string"
  );
}

export function validComment(value: unknown): value is ReviewComment {
  if (!record(value)) return false;
  const carried = value.carriedFrom !== undefined;
  const validSelection =
    value.target === "file"
      ? value.side === "additions" &&
        value.start === 0 &&
        value.end === 0 &&
        // Carried comments keep their original code; other file comments have none.
        (value.code === "" || carried)
      : (value.target === undefined || value.target === "lines") &&
        !carried &&
        validLines(value.side, value.start, value.end);
  return (
    typeof value.id === "string" &&
    typeof value.diffId === "string" &&
    typeof value.versionId === "string" &&
    typeof value.path === "string" &&
    diffMode(value.scope) &&
    typeof value.fingerprint === "string" &&
    validSelection &&
    typeof value.code === "string" &&
    typeof value.body === "string" &&
    value.body.trim().length > 0 &&
    (value.status === "open" || value.status === "resolved") &&
    typeof value.createdAt === "number" &&
    Number.isFinite(value.createdAt) &&
    (value.origin === undefined || validOrigin(value.origin)) &&
    (value.anchor === undefined || validAnchor(value.anchor)) &&
    (!carried || validCarriedFrom(value.carriedFrom))
  );
}

export function parseComments(raw: string | null): ReviewComment[] {
  try {
    const value: unknown = JSON.parse(raw ?? "[]");
    return Array.isArray(value) ? value.filter(validComment) : [];
  } catch {
    return [];
  }
}

function xml(value: string) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

function attribute(value: string) {
  return xml(value)
    .replaceAll("\n", "&#10;")
    .replaceAll("\r", "&#13;")
    .replaceAll("\t", "&#9;");
}

function changeName(status: string) {
  switch (status) {
    case "A":
      return "added";
    case "M":
      return "modified";
    case "D":
      return "deleted";
    case "R":
      return "renamed";
    case "C":
      return "copied";
    case "T":
      return "type-changed";
    case "U":
      return "conflicted";
    case "?":
      return "untracked";
    default:
      return status;
  }
}

function snapshotKey(origin: ReviewOrigin | undefined) {
  return origin
    ? JSON.stringify([
        origin.source,
        origin.diffId,
        origin.versionId,
        origin.repository,
        origin.branch,
        origin.head,
        origin.revision,
      ])
    : "unknown";
}

function placement(comment: ReviewComment): CommentPlacement {
  return (
    comment.placement ?? {
      applicability: "unknown",
      side: comment.side,
      start: comment.start,
      end: comment.end,
    }
  );
}

// Exports comments using their server-computed placements. Unresolved exports
// include every open comment; stale and carried ones are tagged.
export function formatComments(
  comments: readonly ReviewComment[],
  includeResolved: boolean,
): string {
  const selected = includeResolved
    ? comments
    : comments.filter((comment) => comment.status === "open");
  if (selected.length === 0) return "";
  const reviews = new Map<
    string,
    {
      origin: ReviewOrigin | undefined;
      files: Map<string, Array<{ id: string; comment: ReviewComment }>>;
    }
  >();
  for (const [index, comment] of selected.entries()) {
    const key = snapshotKey(comment.origin);
    const review = reviews.get(key) ?? {
      origin: comment.origin,
      files: new Map<string, Array<{ id: string; comment: ReviewComment }>>(),
    };
    const file = review.files.get(comment.path) ?? [];
    file.push({ id: `C${index + 1}`, comment });
    review.files.set(comment.path, file);
    reviews.set(key, review);
  }
  const stale =
    "Comments marked stale were written against earlier code; verify they still apply before acting, and report any that no longer apply using the comment ID.";
  const instruction = [
    includeResolved
      ? "Address every open review comment."
      : "Address every unresolved review comment.",
    "Inspect the current working tree before editing because code and line numbers describe the reviewed snapshot.",
    "Preserve unrelated changes.",
    ...(includeResolved
      ? [
          "Resolved comments are context only; do not act on them.",
          stale,
          "If an open comment cannot be applied, report it using its comment ID.",
        ]
      : [stale]),
  ].join(" ");
  const lines = [
    '<code-review-comments version="2">',
    `  <instructions>${xml(instruction)}</instructions>`,
  ];
  for (const review of reviews.values()) {
    const origin = review.origin;
    if (origin) {
      const head = origin.head ? ` head="${attribute(origin.head)}"` : "";
      lines.push(
        `  <review source="${origin.source}" repository="${attribute(origin.repository)}" branch="${attribute(origin.branch)}"${head} revision="${attribute(origin.revision)}">`,
      );
    } else {
      lines.push('  <review origin="unknown">');
    }
    for (const [path, commentsForFile] of review.files) {
      const fileOrigin = commentsForFile[0]?.comment.origin?.file;
      const oldPath = fileOrigin?.oldPath
        ? ` old-path="${attribute(fileOrigin.oldPath)}"`
        : "";
      const change = fileOrigin
        ? ` change="${attribute(changeName(fileOrigin.status))}"`
        : "";
      lines.push(`    <file path="${attribute(path)}"${oldPath}${change}>`);
      for (const { id, comment } of commentsForFile) {
        const place = placement(comment);
        const staleComment =
          place.applicability === "outdated" ||
          place.applicability === "unchanged" ||
          comment.carriedFrom !== undefined;
        const state = `scope="${comment.scope}" status="${comment.status}" applicability="${place.applicability}"${staleComment ? ' stale="true"' : ""}`;
        if (comment.target === "file") {
          const carried = comment.carriedFrom;
          if (carried) {
            lines.push(
              `      <comment id="${id}" selection="file" ${state} original-line="${carried.start}" original-end-line="${carried.end}" original-side="${carried.side}">`,
              `        <code>${xml(comment.code)}</code>`,
            );
          } else {
            lines.push(`      <comment id="${id}" selection="file" ${state}>`);
          }
          lines.push(
            `        <body>${xml(comment.body)}</body>`,
            "      </comment>",
          );
          continue;
        }
        const inline =
          place.applicability === "anchored" || place.applicability === "moved";
        const start = inline ? place.start : comment.start;
        const end = inline ? place.end : comment.end;
        const selection = start === end ? "single-line" : "range";
        lines.push(
          `      <comment id="${id}" selection="${selection}" line="${start}" end-line="${end}" side="${comment.side}" ${state}>`,
          `        <code>${xml(comment.code)}</code>`,
          `        <body>${xml(comment.body)}</body>`,
          "      </comment>",
        );
      }
      lines.push("    </file>");
    }
    lines.push("  </review>");
  }
  lines.push("</code-review-comments>");
  return lines.join("\n");
}
