import { useIdentity } from "./identity-context.tsx";
import { shellPolicy, type ShellPolicy } from "./shell-policy.ts";

const unresolved: ShellPolicy = {
  reviewNavigation: false,
  reviewColumn: "multiple",
  collection: null,
};

export function useShellPolicy(): ShellPolicy {
  const { state } = useIdentity();
  return state.status === "ready" ? shellPolicy(state.bootstrap) : unresolved;
}
