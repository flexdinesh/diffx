import type { ApiContext } from "@diffx/api";
import { ChevronDownIcon, GitBranchIcon, GitForkIcon } from "lucide-react";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuHeader,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  contextChangeLabel,
  contextCheckoutLabel,
  contextDetail,
  contextDiagnostics,
  contextIsLinkedWorktree,
  contextIsPiped,
  contextTimestampLabel,
} from "./project-picker.ts";
import {
  projectGroups,
  projectKey,
  relativeAge,
  streamKey,
  streamMeta,
  streamTitle,
  type ProjectGroup,
} from "./project-nav.ts";
import { NewMark, useNow } from "./ProjectsNav.tsx";
import { useCatalog } from "./session-context.tsx";

/**
 * The open review's repository, branch and worktree. Repository and branch
 * open menus of current streams, beside the review column.
 */
export function ContextIdentity({ current }: { current: ApiContext }) {
  return (
    <div className="context-identity" title={contextDiagnostics(current)}>
      {contextIsPiped(current) ? (
        <span className="context-identity-static">
          <ContextIcon piped size="md" />
          <span className="context-identity-name">Piped</span>
          <span className="context-identity-branch">
            · {contextTimestampLabel(current)}
          </span>
        </span>
      ) : (
        <ContextSwitcher current={current} />
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

function ContextSwitcher({ current }: { current: ApiContext }) {
  const { heads, seen, openStream } = useCatalog();
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen);
  const repository = groups.find((group) => group.id === projectKey(current));
  const open = (head: ApiContext | undefined) => {
    if (head) openStream(head);
  };
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              variant="shell"
              touch
              className="context-identity-repo"
              aria-label={`Repository ${current.name}. Switch repository`}
            />
          }
        >
          <ContextIcon piped={false} size="md" />
          <span className="context-identity-name">{current.name}</span>
          <ChevronDownIcon
            className="text-muted max-md:hidden"
            aria-hidden="true"
          />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          className="context-switch-menu"
          aria-label="Switch repository"
        >
          <DropdownMenuHeader aria-hidden="true">
            Repositories
          </DropdownMenuHeader>
          <DropdownMenuSeparator />
          <DropdownMenuRadioGroup
            aria-label="Repositories"
            value={repository?.id ?? ""}
            onValueChange={(id) =>
              open(groups.find((group) => group.id === id)?.streams[0]?.context)
            }
          >
            {groups.map((group) => (
              <RepositoryItem key={group.id} group={group} now={now} />
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <span className="context-identity-separator" aria-hidden="true">
        /
      </span>
      {repository ? (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                type="button"
                variant="shell"
                touch
                className="context-identity-stream"
                aria-label={`Branch ${contextDetail(current)}. Switch branch`}
              />
            }
          >
            <GitBranchIcon className="size-icon-sm" aria-hidden="true" />
            <span className="context-identity-branch">
              {contextDetail(current)}
            </span>
            <ChevronDownIcon
              className="text-muted max-md:hidden"
              aria-hidden="true"
            />
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="context-switch-menu"
            aria-label={`Switch ${repository.name} branch`}
          >
            <DropdownMenuHeader aria-hidden="true">
              {repository.name}
            </DropdownMenuHeader>
            <DropdownMenuSeparator />
            <DropdownMenuRadioGroup
              aria-label={`${repository.name} branches`}
              value={streamKey(current)}
              onValueChange={(id) =>
                open(
                  repository.streams.find((stream) => stream.id === id)
                    ?.context,
                )
              }
            >
              {repository.streams.map(({ id, context, unseen }) => (
                <DropdownMenuRadioItem
                  key={id}
                  value={id}
                  className="flex-col items-stretch gap-0"
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    <GitBranchIcon aria-hidden="true" />
                    <span className="min-w-0 flex-1 truncate">
                      {streamTitle(context)}
                    </span>
                    {unseen && <NewMark />}
                    {typeof context.changedFileCount === "number" && (
                      <CountBadge
                        count={context.changedFileCount}
                        label={contextChangeLabel(context)}
                      />
                    )}
                  </span>
                  <span className="truncate text-xs text-muted">
                    {streamMeta(context, repository.multipleHosts, now)}
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      ) : (
        <span className="context-identity-static">
          <GitBranchIcon className="size-icon-sm" aria-hidden="true" />
          <span className="context-identity-branch">
            {contextDetail(current)}
          </span>
        </span>
      )}
    </>
  );
}

function RepositoryItem({ group, now }: { group: ProjectGroup; now: number }) {
  return (
    <DropdownMenuRadioItem value={group.id}>
      <ContextIcon piped={group.kind === "piped"} tone="muted" />
      <span className="min-w-0 flex-1 truncate font-medium">{group.name}</span>
      {group.streams.some((stream) => stream.unseen) && <NewMark />}
      <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
        {relativeAge(group.lastActivity, now)}
      </span>
    </DropdownMenuRadioItem>
  );
}
