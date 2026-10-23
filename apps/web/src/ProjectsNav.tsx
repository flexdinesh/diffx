import { ProjectsSkeleton } from "./WorkspaceLoading.tsx";
import type { ApiContext } from "@diffx/api";
import {
  FolderGit2Icon,
  FolderTreeIcon,
  GitBranchIcon,
  HistoryIcon,
  SearchIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { DisclosureIcon } from "@/components/disclosure";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { IconButton } from "@/components/icon-button";
import { ListRow } from "@/components/list-row";
import { Notice } from "@/components/notice";
import { PanelHeader } from "@/components/panel-header";
import { Scrim } from "@/components/scrim";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuHeader,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { pluralize } from "./format.ts";
import { save, saved } from "./preferences.ts";
import {
  contextChangeLabel,
  contextIsLinkedWorktree,
  contextIsPiped,
  contextWorktreeName,
} from "./project-picker.ts";
import { contextActivity } from "./context-activity.ts";
import {
  newerHead,
  projectGroups,
  relativeAge,
  streamKey,
  streamMeta,
  streamTitle,
  type ProjectGroup,
} from "./project-nav.ts";
import { useCatalog, useSession } from "./session-context.tsx";
import { streamPath } from "./review-route.ts";
import { useSidebarState } from "./sidebar-context.tsx";

/** Compact entry point; desktop view controls live in the activity rail. */
export function ProjectsToggle() {
  const sidebar = useSidebarState();
  const expanded = sidebar.view === "projects" && sidebar.expanded;
  return (
    <div className="topbar-lead">
      <IconButton
        id="projects-toggle"
        label={expanded ? "Hide repos" : "Show repos"}
        variant="shell"
        touch
        aria-controls="projects-nav"
        aria-expanded={expanded}
        onClick={() => sidebar.selectView("projects")}
      >
        <FolderGit2Icon aria-hidden="true" />
      </IconButton>
    </div>
  );
}

export function HeaderBrand() {
  return (
    <>
      <a className="brand" href="/" aria-label="diffx home">
        <span>diffx</span>
      </a>
      <Separator
        variant="chrome"
        orientation="vertical"
        className="header-divider"
      />
    </>
  );
}

function useNow() {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

function readCollapsedGroups() {
  try {
    const data: unknown = JSON.parse(saved("projects-closed-groups") ?? "[]");
    return new Set(
      Array.isArray(data)
        ? data.filter((value) => typeof value === "string")
        : [],
    );
  } catch {
    return new Set<string>();
  }
}

export function ProjectsNav() {
  const sidebar = useSidebarState();
  const { heads, current, openStream, openPicker, seen, collecting } =
    useCatalog();
  const [closed, setClosed] = useState(readCollapsedGroups);
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen);
  const currentStream = current ? streamKey(current) : "";
  const expanded = sidebar.view === "projects" && sidebar.expanded;
  useEffect(() => {
    save("projects-closed-groups", JSON.stringify([...closed]));
  }, [closed]);
  const toggleGroup = (id: string) =>
    setClosed((value) => {
      const next = new Set(value);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const open = (event: MouseEvent<HTMLElement>, head: ApiContext) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
      return;
    event.preventDefault();
    openStream(head, { preserveNavigation: true });
  };
  return (
    <>
      {sidebar.mobile && sidebar.open && sidebar.view === "projects" && (
        <Scrim label="Close repos" onDismiss={sidebar.closeMobile} />
      )}
      <div
        id="projects-nav"
        className={
          sidebar.open && sidebar.view === "projects"
            ? "projects-nav open"
            : "projects-nav"
        }
        aria-label="Repos"
        role={sidebar.mobile && expanded ? "dialog" : "navigation"}
        aria-modal={sidebar.mobile && expanded ? "true" : undefined}
        hidden={!expanded}
      >
        <PanelHeader
          title="Repos"
          actions={
            <>
              <IconButton
                id="projects-browse"
                label="Search reviews"
                shortcut={{ mod: true, key: "K" }}
                touch
                onClick={() => {
                  sidebar.closeMobile();
                  openPicker();
                }}
              >
                <SearchIcon aria-hidden="true" />
              </IconButton>
              <IconButton
                label="Close repos"
                className="lg:hidden"
                touch
                onClick={sidebar.closeMobile}
              >
                <XIcon aria-hidden="true" />
              </IconButton>
            </>
          }
        />
        <div className="projects-scroll">
          {groups.length ? (
            <ul className="projects-list">
              {groups.map((group) => (
                <ProjectItem
                  key={group.id}
                  group={group}
                  expanded={!closed.has(group.id)}
                  onToggle={() => toggleGroup(group.id)}
                  currentStream={currentStream}
                  now={now}
                  onOpen={open}
                />
              ))}
            </ul>
          ) : (
            <ProjectsEmpty
              loaded={heads.loaded}
              collecting={collecting}
              error={heads.error}
              onRetry={heads.retry}
            />
          )}
        </div>
      </div>
    </>
  );
}

function ProjectItem({
  group,
  expanded,
  onToggle,
  currentStream,
  now,
  onOpen,
}: {
  group: ProjectGroup;
  expanded: boolean;
  onToggle: () => void;
  currentStream: string;
  now: number;
  onOpen: (event: MouseEvent<HTMLElement>, head: ApiContext) => void;
}) {
  const unseen = group.streams.some((stream) => stream.unseen);
  const listId = `project-${group.id}`;
  return (
    <li>
      <ListRow
        type="button"
        className="mt-1"
        aria-expanded={expanded}
        aria-controls={listId}
        title={group.name}
        onClick={onToggle}
      >
        <span className="inline-flex text-muted">
          <DisclosureIcon open={expanded} />
        </span>
        <ContextIcon piped={group.kind === "piped"} tone="muted" />
        <span className="min-w-0 flex-1 truncate font-medium">
          {group.name}
        </span>
        {unseen && !expanded && <NewMark />}
        <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
          {relativeAge(group.lastActivity, now)}
        </span>
      </ListRow>
      {expanded && (
        <ul id={listId} className="stream-list">
          {group.streams.map(({ id, context, unseen: fresh }) => {
            const current = id === currentStream;
            return (
              <li key={id}>
                <ListRow
                  depth={1}
                  className="flex-col items-stretch"
                  render={<a href={streamPath(context)} />}
                  aria-current={current ? "page" : undefined}
                  title={`${streamTitle(context)} · ${contextChangeLabel(context)}`}
                  onClick={(event) => onOpen(event, context)}
                >
                  <span className="flex min-w-0 items-center gap-1.5">
                    {!contextIsPiped(context) && (
                      <GitBranchIcon
                        className={current ? undefined : "text-muted"}
                        aria-hidden="true"
                      />
                    )}
                    <span className="min-w-0 flex-1 truncate">
                      {streamTitle(context)}
                    </span>
                    {fresh && !current && <NewMark />}
                    {typeof context.changedFileCount === "number" && (
                      <CountBadge
                        count={context.changedFileCount}
                        label={contextChangeLabel(context)}
                      />
                    )}
                  </span>
                  <span className="stream-meta truncate text-xs text-muted">
                    {streamMeta(context, group.multipleHosts, now)}
                  </span>
                </ListRow>
              </li>
            );
          })}
        </ul>
      )}
    </li>
  );
}

function NewMark() {
  return <StatusDot tone="accent" size="sm" label="New changes" />;
}

function ProjectsEmpty({
  loaded,
  collecting,
  error,
  onRetry,
}: {
  loaded: boolean;
  collecting: boolean;
  error: string;
  onRetry: () => void;
}) {
  if (error) return <ErrorState message={error} onRetry={onRetry} />;
  if (collecting || !loaded)
    return (
      <div role="status" aria-busy="true">
        <ProjectsSkeleton />
        <p className="visually-hidden">
          {collecting ? "Collecting repos…" : "Loading repos…"}
        </p>
      </div>
    );
  return (
    <EmptyState
      role="status"
      aria-busy={false}
      heading="p"
      title="No current changes"
      description="Agent hooks and diffx sync publish changed checkouts here."
    />
  );
}

/** Lists unread heads; choosing one opens it and clears its mark. */
export function NewChangesButton() {
  const { unreadHeads, openStream } = useCatalog();
  const now = useNow();
  if (!unreadHeads.length) return null;
  const count = unreadHeads.length;
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            id="new-changes"
            variant="shell"
            size="sm"
            touch
            aria-label={`New changes (${pluralize(count, "stream")})`}
          />
        }
      >
        <HistoryIcon aria-hidden="true" />
        <span>
          {count}
          <span className="max-md:hidden"> new</span>
        </span>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        side="bottom"
        align="end"
        className="new-changes-menu"
        aria-label="New changes"
      >
        <DropdownMenuHeader aria-hidden="true">New changes</DropdownMenuHeader>
        <DropdownMenuSeparator />
        {unreadHeads.map((context) => (
          <NewChangeItem
            key={context.id}
            context={context}
            now={now}
            onOpen={() => openStream(context)}
          />
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function NewChangeItem({
  context,
  now,
  onOpen,
}: {
  context: ApiContext;
  now: number;
  onOpen: () => void;
}) {
  const piped = contextIsPiped(context);
  const repository = piped ? "Piped" : context.name;
  const branch = streamTitle(context);
  const worktree = contextIsLinkedWorktree(context)
    ? contextWorktreeName(context)
    : context.observation?.linkedWorktree === false
      ? "Primary checkout"
      : "";
  const age = relativeAge(contextActivity(context), now);
  return (
    <DropdownMenuItem
      className="flex-col items-stretch gap-0"
      aria-label={[
        repository,
        branch,
        worktree,
        contextChangeLabel(context),
        age,
      ]
        .filter(Boolean)
        .join(", ")}
      onClick={onOpen}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        <ContextIcon piped={piped} />
        <span className="min-w-0 flex-1 truncate font-medium">
          {repository}
        </span>
        <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
          {age}
        </span>
      </span>
      <span className="flex min-w-0 items-center gap-1.5 text-xs text-secondary">
        {!piped && <GitBranchIcon aria-hidden="true" />}
        <span className="min-w-0 flex-1 truncate">{branch}</span>
        {typeof context.changedFileCount === "number" && (
          <CountBadge count={context.changedFileCount} />
        )}
      </span>
      {worktree && (
        <span className="flex min-w-0 items-center gap-1.5 text-xs text-secondary">
          <FolderTreeIcon aria-hidden="true" />
          <span className="min-w-0 flex-1 truncate">{worktree}</span>
        </span>
      )}
    </DropdownMenuItem>
  );
}

/** Announces a newer head for the viewed stream without replacing the review. */
export function NewerSnapshotNotice() {
  const session = useSession();
  const { heads, openStream } = useCatalog();
  const newer = newerHead(session, heads.contexts);
  const now = useNow();
  return (
    <Notice
      hidden={!newer}
      icon={newer && <HistoryIcon aria-hidden="true" />}
      action={
        newer && (
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={() => openStream(newer, { restoreFocus: false })}
          >
            View latest
          </Button>
        )
      }
    >
      {newer &&
        `Newer snapshot of ${streamTitle(newer)} · ${contextChangeLabel(newer)} · ${relativeAge(contextActivity(newer), now)}`}
    </Notice>
  );
}
