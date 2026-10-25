import type { ApiBootstrap } from "@diffx/api";
import { useIdentity } from "./identity-context.tsx";

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
};

export function shellPolicy(bootstrap: ApiBootstrap): ShellPolicy {
  return {
    reviewNavigation:
      bootstrap.mode === "remote" || bootstrap.input === "checkouts",
  };
}

const unresolved: ShellPolicy = { reviewNavigation: false };

export function useShellPolicy(): ShellPolicy {
  const { state } = useIdentity();
  return state.status === "ready" ? shellPolicy(state.bootstrap) : unresolved;
}
