import type { ApiCommitSummary } from "@diffx/api";

/** A commit to view: named by the URL, or remembered for the stream. */
export type CommitRequest =
  | { kind: "explicit"; oid: string }
  | { kind: "saved"; oid: string; patchId: string | null };

/** The read-only commit lens over a branch's captured commits. */
export interface CommitView {
  /** The requested commit; it may be absent from this snapshot. */
  oid: string;
  /** The shown commit; null while loading or when unavailable. */
  summary: ApiCommitSummary | null;
  status: "loading" | "ready" | "unavailable" | "error";
  detail: string;
  commits: readonly ApiCommitSummary[];
  /** Older commits of the branch that were not captured. */
  omitted: number;
  /** Explains a remembered commit that could not be followed. */
  notice: string;
}

export interface SavedCommit {
  oid: string;
  patchId: string | null;
}

export function readSavedCommit(raw: string | null): SavedCommit | null {
  if (raw === null) return null;
  try {
    const data: unknown = JSON.parse(raw);
    if (
      typeof data !== "object" ||
      data === null ||
      !("oid" in data) ||
      typeof data.oid !== "string" ||
      !data.oid
    )
      return null;
    const patchId =
      "patchId" in data && typeof data.patchId === "string"
        ? data.patchId
        : null;
    return { oid: data.oid, patchId };
  } catch {
    return null;
  }
}

export function savedCommit(commit: ApiCommitSummary): string {
  return JSON.stringify({ oid: commit.oid, patchId: commit.patchId ?? null });
}

export const shortOid = (oid: string) => oid.slice(0, 7);

/**
 * An explicit commit resolves only to itself. A remembered commit follows a
 * rebase by patch ID, else the review restarts at the oldest commit.
 */
export function resolveCommit(
  commits: readonly ApiCommitSummary[],
  request: CommitRequest,
): { commit: ApiCommitSummary | null; notice: string } {
  const exact = commits.find((commit) => commit.oid === request.oid);
  if (exact || request.kind === "explicit")
    return { commit: exact ?? null, notice: "" };
  const { patchId } = request;
  const rewritten = patchId
    ? commits.find((commit) => commit.patchId === patchId)
    : undefined;
  if (rewritten) return { commit: rewritten, notice: "" };
  const oldest = commits[0] ?? null;
  return {
    commit: oldest,
    notice: oldest
      ? `Commit ${shortOid(request.oid)} is not in this snapshot. Showing the oldest commit.`
      : "",
  };
}

/** The neighbor `delta` commits away, or null past either end. */
export function stepCommit(
  commits: readonly ApiCommitSummary[],
  oid: string,
  delta: number,
) {
  const index = commits.findIndex((commit) => commit.oid === oid);
  if (index < 0) return null;
  return commits[index + delta] ?? null;
}

/** Filters by subject, SHA prefix or author. */
export function commitMatches(commit: ApiCommitSummary, query: string) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  const subject = commit.subject.toLowerCase();
  const author = commit.authorName.toLowerCase();
  return words.every(
    (word) =>
      subject.includes(word) ||
      author.includes(word) ||
      commit.oid.startsWith(word),
  );
}

/** Why a commit view shows no diff; null when it has one to show. */
export function commitEmptyState(
  commit: CommitView,
): { title: string; description: string } | null {
  if (commit.status === "error")
    return { title: "Cannot load commits", description: commit.detail };
  if (commit.status === "unavailable")
    return {
      title: "Commit not in this snapshot",
      description: `Commit ${shortOid(commit.oid)} was not captured with this snapshot. Choose a commit above.`,
    };
  if (commit.summary?.merge)
    return {
      title: "Merge commit",
      description:
        "Merge commits are listed without a diff. Step to a neighboring commit, or view the branch's changes.",
    };
  return null;
}
