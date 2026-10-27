import type { ApiBootstrap } from "@diffx/api";

/**
 * What the review shell offers, resolved once from the server's bootstrap.
 * Components read the policy, never the deployment mode or input.
 */
export type ShellPolicy = {
  /**
   * Switching between reviews: the Reviews column and rail, header
   * repository/branch comboboxes, search (Cmd/Ctrl+K) and sync. A local
   * capture (piped stdin or a fixture) serves one fixed diff with nothing to
   * switch to, so it shows only that diff and its file sidebar.
   */
  reviewNavigation: boolean;
  /**
   * When the Reviews column, rail and compact drawer toggle show. A remote
   * server always shows them; a local checkout process waits until there are
   * two current reviews to switch between.
   */
  reviewColumn: "always" | "multiple";
  /** What Sync collects: one selected checkout or every discovered one. */
  collection: "checkout" | "checkouts" | null;
};

export function shellPolicy(bootstrap: ApiBootstrap): ShellPolicy {
  if (bootstrap.mode === "remote")
    return { reviewNavigation: true, reviewColumn: "always", collection: null };
  const collection = bootstrap.input === "capture" ? null : bootstrap.input;
  return {
    reviewNavigation: collection !== null,
    reviewColumn: "multiple",
    collection,
  };
}

/** Whether the Reviews column shows while it lists `streams` streams. */
export function showsReviewColumn(policy: ShellPolicy, streams: number) {
  return (
    policy.reviewNavigation && (policy.reviewColumn === "always" || streams > 1)
  );
}
