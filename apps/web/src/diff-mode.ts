import type { ApiContext, ApiRepositoryDiff } from "@diffx/api";
import { contextIsPiped } from "./project-picker.ts";
export type DiffMode = ApiRepositoryDiff["mode"];
export type ComparisonKind = NonNullable<ApiRepositoryDiff["comparisonKind"]>;

export function isDiffMode(value: unknown): value is DiffMode {
  return (
    value === "all" ||
    value === "staged" ||
    value === "unstaged" ||
    value === "uncommitted" ||
    value === "committed"
  );
}

/** Captured semantics, including the server's normalized legacy `all` scope. */
export function comparisonKind(
  context: ApiContext,
  scope: DiffMode,
): ComparisonKind {
  const captured = context.scopeSummaries?.find(
    (summary) => summary.scope === scope,
  );
  if (captured) return captured.comparisonKind;
  if (scope !== "all") return scope;
  // Optional metadata permits older captures/servers. Never inspect live Git.
  if (contextIsPiped(context)) return "patch";
  if (context.observation?.comparison?.kind === "branch")
    return context.capabilities.diff.stagingMetadata.state === "enabled"
      ? "combined"
      : "committed";
  return "uncommitted";
}

export function defaultScope(context: ApiContext): DiffMode {
  const scopes = context.capabilities.diff.scopes.values;
  const find = (kind: ComparisonKind) =>
    scopes.find((scope) => comparisonKind(context, scope) === kind);
  const uncommitted = find("uncommitted");
  const localEdits = context.scopeSummaries?.some(
    (summary) =>
      (summary.comparisonKind === "staged" ||
        summary.comparisonKind === "unstaged") &&
      summary.fileCount > 0,
  );
  if (localEdits && uncommitted) return uncommitted;
  return find("committed") ?? uncommitted ?? scopes[0] ?? "all";
}

export function selectedScope(
  context: ApiContext,
  explicit: string | null,
  saved: string | null,
) {
  const scopes = context.capabilities.diff.scopes.values;
  const requested =
    explicit ??
    (isDiffMode(saved) && scopes.includes(saved)
      ? saved
      : defaultScope(context));
  return {
    mode: isDiffMode(requested) ? requested : defaultScope(context),
    requestedScope: requested,
    scopeAvailable: isDiffMode(requested) && scopes.includes(requested),
  };
}

export function comparisonLabel(kind: ComparisonKind) {
  switch (kind) {
    case "combined":
      return "Combined";
    case "committed":
      return "Branch changes";
    case "patch":
      return "Piped patch";
    default:
      return "Uncommitted";
  }
}

export function emptyComparison(context: ApiContext, scope: DiffMode) {
  const kind = comparisonKind(context, scope);
  const titles: Record<ComparisonKind, string> = {
    uncommitted: "No net uncommitted changes",
    staged: "Nothing staged",
    unstaged: "No unstaged changes",
    committed: "No branch changes",
    combined: "No net combined changes",
    patch: "No file changes in this input",
  };
  const otherChanges =
    context.scopeSummaries?.filter(
      (summary) =>
        summary.scope !== scope &&
        summary.fileCount > 0 &&
        context.capabilities.diff.scopes.values.includes(summary.scope),
    ) ?? [];
  const alternatives = [
    ...new Set(
      otherChanges.map(({ comparisonKind: other }) =>
        other === "staged"
          ? "Staged"
          : other === "unstaged"
            ? "Unstaged"
            : comparisonLabel(other),
      ),
    ),
  ];
  return {
    title: titles[kind],
    description: alternatives.length
      ? `This comparison has no net file changes. Captured changes are available in ${alternatives.join(", ")}. Choose a view above to review them.`
      : "No changes were collected for this comparison. Collect another snapshot with diffx or diffx sync.",
  };
}
