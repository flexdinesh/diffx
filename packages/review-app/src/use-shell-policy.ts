import { createContext, useContext } from "react";
import { useIdentity } from "./identity-context.tsx";
import { shellPolicy, type ShellPolicy } from "./shell-policy.ts";

const unresolved: ShellPolicy = {
  reviewNavigation: false,
  reviewColumn: "multiple",
  collection: null,
  reviewMarks: true,
  serverStatus: false,
};

/** A page whose shell is not the bootstrap's, such as a public preview. */
export const ShellPolicyContext = createContext<ShellPolicy | null>(null);

export function useShellPolicy(): ShellPolicy {
  const { state } = useIdentity();
  const page = useContext(ShellPolicyContext);
  if (page) return page;
  return state.status === "ready" ? shellPolicy(state.bootstrap) : unresolved;
}
