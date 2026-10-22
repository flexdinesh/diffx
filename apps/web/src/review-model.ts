import type {
  ApiChangedFile as ChangedFile,
  ApiRepositoryDiff as RepositoryDiff,
} from "@diffx/api";
import type { ReviewComment } from "./review-comments.ts";

export {
  commentContext,
  formatComments,
  lineContext,
  parseComments,
} from "./review-comments.ts";
export type { ReviewComment, ReviewOrigin } from "./review-comments.ts";

export type CommentAnnotation =
  | { kind: "saved"; comment: ReviewComment }
  | { kind: "draft" };

export function createCommentId(
  bytes = crypto.getRandomValues(new Uint8Array(16)),
) {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join(
    "",
  );
}

export type CommentPlacement = NonNullable<ReviewComment["placement"]>;
export type Applicability = CommentPlacement["applicability"];

/**
 * Server placement in the viewed snapshot. Comments saved since the last
 * comment read lack one; they belong to the viewed snapshot by construction,
 * and anything else unplaced is conservatively outdated.
 */
export function placementFor(
  comment: ReviewComment,
  repository: RepositoryDiff | null,
): CommentPlacement {
  if (comment.placement) return comment.placement;
  return {
    applicability:
      repository === null
        ? "unknown"
        : comment.scope !== repository.mode
          ? "other-scope"
          : comment.versionId === repository.versionId
            ? "anchored"
            : "outdated",
    side: comment.side,
    start: comment.start,
    end: comment.end,
  };
}

/** Anchored and moved comments render on their lines. */
export function placedInline(applicability: Applicability) {
  return applicability === "anchored" || applicability === "moved";
}

/** Outdated comments stay visible at file level, never on lines. */
export function placedOutdated(applicability: Applicability) {
  return applicability === "outdated" || applicability === "unchanged";
}

/** Written against earlier code: outdated now, or kept open from an outdated state. */
export function commentStale(
  comment: ReviewComment,
  repository: RepositoryDiff | null,
) {
  return (
    comment.carriedFrom !== undefined ||
    placedOutdated(placementFor(comment, repository).applicability)
  );
}

/** Stream comments follow renames: the viewed file may carry the comment's path as its old path. */
export function commentInFile(
  comment: ReviewComment,
  file: Pick<ChangedFile, "path" | "oldPath">,
) {
  return comment.path === file.path || comment.path === file.oldPath;
}
