import type { ApiContext } from "@diffx/api";
import { GitBranchIcon, GitForkIcon } from "lucide-react";
import { ContextIcon } from "@/components/context-icon";
import {
  contextCheckoutLabel,
  contextDetail,
  contextDiagnostics,
  contextIsLinkedWorktree,
  contextIsPiped,
  contextTimestampLabel,
} from "./project-picker.ts";

/** The open review's repository, branch and worktree; switching lives in the projects drawer. */
export function ContextIdentity({ current }: { current: ApiContext }) {
  const piped = contextIsPiped(current);
  return (
    <div className="context-identity" title={contextDiagnostics(current)}>
      <ContextIcon piped={piped} size="md" />
      <span className="context-identity-name">
        {piped ? "Piped" : current.name}
      </span>
      {piped ? (
        <span className="context-identity-branch">
          · {contextTimestampLabel(current)}
        </span>
      ) : (
        <>
          <span className="context-identity-separator" aria-hidden="true">
            /
          </span>
          <GitBranchIcon className="size-icon-sm" aria-hidden="true" />
          <span className="context-identity-branch">
            {contextDetail(current)}
          </span>
        </>
      )}
      {contextIsLinkedWorktree(current) && (
        <span
          className="context-identity-worktree"
          role="img"
          aria-label={contextCheckoutLabel(current)}
          title={contextCheckoutLabel(current)}
        >
          <GitForkIcon className="size-icon-sm" aria-hidden="true" />
          <span className="context-identity-worktree-name" aria-hidden="true">
            Worktree:{" "}
            {current.worktreeName || current.observation?.worktreeName}
          </span>
        </span>
      )}
    </div>
  );
}
