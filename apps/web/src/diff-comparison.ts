import type { ApiContext } from "@diffx/api";
import {
  comparisonKind,
  type ComparisonKind,
  type DiffMode,
} from "./diff-mode.ts";

export function diffComparison(
  context: ApiContext,
  mode: DiffMode,
  repositoryHead: string | null | undefined,
  capturedKind?: ComparisonKind,
) {
  const kind = capturedKind ?? comparisonKind(context, mode);
  const observation = context.observation;
  const comparison = observation?.comparison;
  const head = observation ? observation.head : repositoryHead;
  const details: { label: string; value: string }[] = [];
  const recorded = (value: string | null | undefined) =>
    value || "Not recorded";
  if (kind === "patch") {
    return {
      summary: "Command output · fixed snapshot",
      description:
        "The supplied patch is shown as captured. No Git comparison was calculated by diffx.",
      details,
    };
  }

  let summary: string;
  let description: string;
  if (kind === "unstaged") {
    summary = "Index → working tree, including untracked files";
    description =
      "Unstaged changes compare the index with the working tree, including untracked files. Committed branch changes are excluded.";
    details.push({ label: "Baseline", value: "Index" });
  } else if (kind === "combined" || kind === "committed") {
    const base =
      comparison?.baseRef.replace(/^refs\/(heads|remotes)\//, "") ||
      "Not recorded";
    const committedOnly = kind === "committed";
    const target = committedOnly ? "HEAD" : "working tree";
    summary = `Merge base (${base} · ${comparison?.mergeBase?.slice(0, 7) || "unknown"}) → ${target}`;
    description = committedOnly
      ? "Committed branch changes compare the merge base with the captured HEAD. Index and working tree changes are excluded."
      : "Combined changes compare the merge base with the working tree, including committed, staged, unstaged, and untracked changes.";
    details.push(
      { label: "Base ref", value: recorded(comparison?.baseRef) },
      { label: "Base commit", value: recorded(comparison?.baseCommit) },
      { label: "Merge base", value: recorded(comparison?.mergeBase) },
    );
  } else {
    const unborn = !!comparison && !comparison.baseCommit && !head;
    const base = unborn ? "Empty tree" : "HEAD";
    summary =
      kind === "staged"
        ? `${base} → index`
        : `${base} → working tree, including untracked files`;
    description =
      kind === "staged"
        ? "Staged changes compare the captured HEAD with the index. Committed branch changes are excluded."
        : "Working changes compare the captured HEAD with the working tree, including staged, unstaged, and untracked changes. Committed branch changes are excluded.";
    if (unborn)
      description =
        "This checkout has no commits. Changes are compared with Git’s empty tree.";
    details.push({ label: "Baseline", value: base });
  }
  details.push(
    {
      label: "HEAD",
      value:
        head ||
        (comparison && !comparison.baseCommit
          ? "Unborn (no commits)"
          : "Not recorded"),
    },
    { label: "Branch", value: recorded(observation?.branch ?? context.branch) },
  );
  return { summary, description, details };
}
