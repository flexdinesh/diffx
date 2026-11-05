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
  /**
   * Reviewed-file marks: file header toggles, Alt+M, Hide reviewed files and
   * review progress. A public pull request preview, read by anyone, has none.
   */
  reviewMarks: boolean;
  /**
   * The footer's server process status. Its metrics need an account on a
   * remote server, so a signed-out reader of a public preview has none.
   */
  serverStatus: boolean;
};

export function shellPolicy(bootstrap: ApiBootstrap): ShellPolicy {
  if (bootstrap.mode === "remote")
    return {
      reviewNavigation: true,
      reviewColumn: "always",
      collection: null,
      reviewMarks: true,
      serverStatus: true,
    };
  const collection = bootstrap.input === "capture" ? null : bootstrap.input;
  return {
    reviewNavigation: collection !== null,
    reviewColumn: "multiple",
    collection,
    reviewMarks: true,
    serverStatus: true,
  };
}

/**
 * A public pull request preview: one fixed, read-only snapshot with its file
 * sidebar, for signed-in and signed-out readers alike.
 */
export function publicPreviewPolicy(bootstrap: ApiBootstrap): ShellPolicy {
  return {
    reviewNavigation: false,
    reviewColumn: "multiple",
    collection: null,
    reviewMarks: false,
    serverStatus: bootstrap.user !== null,
  };
}

/** Whether the Reviews column shows while it lists `streams` streams. */
export function showsReviewColumn(policy: ShellPolicy, streams: number) {
  return (
    policy.reviewNavigation && (policy.reviewColumn === "always" || streams > 1)
  );
}
