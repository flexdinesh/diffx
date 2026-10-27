import type { ApiContext } from "@diffx/api";
import { ChevronDownIcon, GitBranchIcon, GitForkIcon } from "lucide-react";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { Button } from "@/components/ui/button";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxSearch,
  ComboboxTrigger,
} from "@/components/ui/combobox";
import {
  contextChangeLabel,
  contextCheckoutLabel,
  contextDetail,
  contextDiagnostics,
  contextIsLinkedWorktree,
  contextTimestampLabel,
} from "./project-picker.ts";
import {
  latestStream,
  projectGroups,
  projectKey,
  relativeAge,
  streamKey,
  streamMatches,
  streamMeta,
  streamTitle,
  type ProjectGroup,
  type ProjectStream,
} from "./project-nav.ts";
import { NewMark, StreamIcon, useNow } from "./ProjectsNav.tsx";
import { useCatalog } from "./session-context.tsx";
import { useShellPolicy } from "./use-shell-policy.ts";

/**
 * The open review's repository, branch and worktree. With review navigation,
 * repository and branch are comboboxes that filter and switch current streams;
 * a capture shows its fixed piped identity.
 */
export function ContextIdentity({ current }: { current: ApiContext }) {
  const { reviewNavigation } = useShellPolicy();
  return (
    <div className="context-identity" title={contextDiagnostics(current)}>
      {!reviewNavigation ? (
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
  const stream = repository?.streams.find(
    (candidate) => candidate.id === streamKey(current),
  );
  return (
    <>
      <Combobox
        items={groups}
        value={repository ?? null}
        autoHighlight
        isItemEqualToValue={(item, value) => item.id === value.id}
        itemToStringLabel={(group) => group.name}
        onValueChange={(group) => {
          const latest = group && latestStream(group);
          if (latest) openStream(latest.context);
        }}
      >
        <ComboboxTrigger
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
          <ContextIcon size="md" />
          <span className="context-identity-name">{current.name}</span>
          <ChevronDownIcon
            className="text-muted max-md:hidden"
            aria-hidden="true"
          />
        </ComboboxTrigger>
        <ComboboxContent className="context-switch-menu">
          <ComboboxSearch
            label="Filter repositories"
            placeholder="Filter repositories…"
          />
          <ComboboxEmpty>No matching repositories</ComboboxEmpty>
          <ComboboxList aria-label="Repositories">
            {(group: ProjectGroup) => (
              <RepositoryItem key={group.id} group={group} now={now} />
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      <span className="context-identity-separator" aria-hidden="true">
        /
      </span>
      {repository ? (
        <Combobox
          items={repository.streams}
          value={stream ?? null}
          autoHighlight
          isItemEqualToValue={(item, value) => item.id === value.id}
          itemToStringLabel={(item) => streamTitle(item.context)}
          filter={streamMatches}
          onValueChange={(item) => {
            if (item) openStream(item.context);
          }}
        >
          <ComboboxTrigger
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
          </ComboboxTrigger>
          <ComboboxContent className="context-switch-menu">
            <ComboboxSearch
              label={`Filter ${repository.name} branches`}
              placeholder="Filter branches, worktrees, sessions…"
            />
            <ComboboxEmpty>No matching branches</ComboboxEmpty>
            <ComboboxList aria-label={`${repository.name} branches`}>
              {(item: ProjectStream) => (
                <BranchItem
                  key={item.id}
                  item={item}
                  multipleHosts={repository.multipleHosts}
                  now={now}
                />
              )}
            </ComboboxList>
          </ComboboxContent>
        </Combobox>
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

function BranchItem({
  item,
  multipleHosts,
  now,
}: {
  item: ProjectStream;
  multipleHosts: boolean;
  now: number;
}) {
  const { context, unseen } = item;
  return (
    <ComboboxItem value={item} className="flex-col items-stretch gap-0">
      <span className="flex min-w-0 items-center gap-1.5">
        <StreamIcon context={context} />
        <span className="min-w-0 flex-1 truncate">{streamTitle(context)}</span>
        {unseen && <NewMark />}
        {typeof context.changedFileCount === "number" && (
          <CountBadge
            count={context.changedFileCount}
            label={contextChangeLabel(context)}
          />
        )}
      </span>
      <span className="truncate text-xs text-muted">
        {streamMeta(context, multipleHosts, now)}
      </span>
    </ComboboxItem>
  );
}

function RepositoryItem({ group, now }: { group: ProjectGroup; now: number }) {
  return (
    <ComboboxItem value={group}>
      <ContextIcon tone="muted" />
      <span className="min-w-0 flex-1 truncate font-medium">{group.name}</span>
      {group.streams.some((stream) => stream.unseen) && <NewMark />}
      <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
        {relativeAge(group.lastActivity, now)}
      </span>
    </ComboboxItem>
  );
}
