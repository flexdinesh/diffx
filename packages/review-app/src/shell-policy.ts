import type { ApiBootstrap } from "@diffx/api";

/**
 * What the review shell offers, resolved once from the server's bootstrap.
 * Components read the policy, never the deployment mode or input.
 */
export type ShellPolicy = {
  /**
   * Switching between reviews: the Review bench at `/`, the header's Reviews
   * breadcrumb, review stepper and repository/branch comboboxes, search
   * (Cmd/Ctrl+K) and sync. A local capture (piped stdin or a fixture) serves
   * one fixed diff with nothing to switch to, so `/` opens that diff with only
   * its file sidebar.
   */
  reviewNavigation: boolean;
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
  /**
   * GitHub pull requests: the bench's follow-repositories guidance and the
   * header branch combobox's pull request group. They need the server's
   * GitHub integration.
   */
  pullRequests: boolean;
};

export function shellPolicy(bootstrap: ApiBootstrap): ShellPolicy {
  if (bootstrap.mode === "remote")
    return {
      reviewNavigation: true,
      collection: null,
      reviewMarks: true,
      serverStatus: true,
      pullRequests: bootstrap.githubEnabled,
    };
  const collection = bootstrap.input === "capture" ? null : bootstrap.input;
  return {
    reviewNavigation: collection !== null,
    collection,
    reviewMarks: true,
    serverStatus: true,
    pullRequests: false,
  };
}

/**
 * A public pull request preview: one fixed, read-only snapshot with its file
 * sidebar, for signed-in and signed-out readers alike.
 */
export function publicPreviewPolicy(bootstrap: ApiBootstrap): ShellPolicy {
  return {
    reviewNavigation: false,
    collection: null,
    reviewMarks: false,
    serverStatus: bootstrap.user !== null,
    pullRequests: false,
  };
}
