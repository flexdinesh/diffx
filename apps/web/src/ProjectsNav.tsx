import { ProjectsSkeleton } from "./WorkspaceLoading.tsx";
import type { ApiContext } from "@diffx/api";
import {
  FolderGit2Icon,
  FolderTreeIcon,
  GitBranchIcon,
  HistoryIcon,
  InboxIcon,
  ListIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  SearchIcon,
  SquareTerminalIcon,
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
  DropdownMenuGroup,
  DropdownMenuHeader,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
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
  recentStreams,
  relativeAge,
  repoMonogram,
  streamKey,
  streamMeta,
  streamTitle,
  type ProjectGroup,
  type StreamGroupMode,
} from "./project-nav.ts";
import { useCatalog, useSession } from "./session-context.tsx";
import { streamPath } from "./review-route.ts";
import { useSidebarState } from "./sidebar-context.tsx";
import { useStreamNavState } from "./stream-nav-context.tsx";

const groupModes = [
  { value: "repo", label: "Group by repo" },
  { value: "recent", label: "Recent first" },
] satisfies { value: StreamGroupMode; label: string }[];

/** Drawer entry point below 1012px, where the review column is hidden. */
export function ProjectsToggle() {
  const sidebar = useSidebarState();
  const expanded = sidebar.drawer === "reviews";
  return (
    <div className="topbar-lead">
      <IconButton
        id="projects-toggle"
        label={expanded ? "Hide reviews" : "Show reviews"}
        variant="shell"
        touch
        aria-controls="projects-nav"
        aria-expanded={expanded}
        onClick={sidebar.toggleReviews}
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

export function useNow() {
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

/** Element ID of a repository's first row, the target when expanding from the rail. */
function repoAnchor(id: string) {
  return `review-repo-${id}`;
}

/** Opens a stream in place unless the click asks the browser for a new tab. */
export function streamClick(event: MouseEvent<HTMLElement>, open: () => void) {
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
    return;
  event.preventDefault();
  open();
}

/**
 * The outermost navigation: every review stream. Desktop shows the list or a
 * repository rail; below 1012px it is the reviews drawer.
 */
export function ProjectsNav() {
  const sidebar = useSidebarState();
  const streams = useStreamNavState();
  const { heads, current, openStream, openPicker, seen, collecting } =
    useCatalog();
  const [closed, setClosed] = useState(readCollapsedGroups);
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen);
  const currentStream = current ? streamKey(current) : "";
  const drawerOpen = sidebar.drawer === "reviews";
  const rail = !sidebar.mobile && streams.state === "rail";
  const { focusRepo, clearFocusRepo } = streams;
  useEffect(() => {
    save("projects-closed-groups", JSON.stringify([...closed]));
  }, [closed]);
  useEffect(() => {
    if (!focusRepo || rail) return;
    setClosed((value) => {
      if (!value.has(focusRepo)) return value;
      const next = new Set(value);
      next.delete(focusRepo);
      return next;
    });
    requestAnimationFrame(() => {
      const target = document.getElementById(repoAnchor(focusRepo));
      target?.scrollIntoView({ block: "nearest" });
      target?.focus();
    });
    clearFocusRepo();
  }, [focusRepo, rail, clearFocusRepo]);
  const toggleGroup = (id: string) =>
    setClosed((value) => {
      const next = new Set(value);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const open = (event: MouseEvent<HTMLElement>, head: ApiContext) =>
    streamClick(event, () => openStream(head));
  if (rail)
    return (
      <StreamRail
        groups={groups}
        currentStream={currentStream}
        onOpenInbox={streams.openInbox}
      />
    );
  return (
    <>
      {drawerOpen && (
        <Scrim label="Close reviews" onDismiss={sidebar.closeMobile} />
      )}
      <div
        id="projects-nav"
        className={drawerOpen ? "projects-nav open" : "projects-nav"}
        aria-label="Reviews"
        role={drawerOpen ? "dialog" : "navigation"}
        aria-modal={drawerOpen ? "true" : undefined}
        hidden={sidebar.mobile && !drawerOpen}
      >
        <PanelHeader
          title="Reviews"
          actions={
            <>
              <GroupModeMenu
                value={streams.group}
                onChange={streams.setGroup}
              />
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
                id="inbox-open"
                label="Open inbox"
                touch
                aria-expanded={streams.inboxOpen}
                aria-controls="inbox"
                onClick={() => {
                  sidebar.closeMobile();
                  streams.openInbox();
                }}
              >
                <InboxIcon aria-hidden="true" />
              </IconButton>
              <IconButton
                id="reviews-collapse"
                label="Collapse reviews"
                className="max-lg:hidden"
                touch
                onClick={streams.collapse}
              >
                <PanelLeftCloseIcon aria-hidden="true" />
              </IconButton>
              <IconButton
                label="Close reviews"
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
          {!groups.length ? (
            <ProjectsEmpty
              loaded={heads.loaded}
              collecting={collecting}
              error={heads.error}
              onRetry={heads.retry}
            />
          ) : streams.group === "recent" ? (
            <RecentList
              groups={groups}
              currentStream={currentStream}
              now={now}
              onOpen={open}
            />
          ) : (
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
          )}
        </div>
      </div>
    </>
  );
}

function GroupModeMenu({
  value,
  onChange,
}: {
  value: StreamGroupMode;
  onChange: (value: StreamGroupMode) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton id="reviews-view" label="Review list view" touch>
            <ListIcon aria-hidden="true" />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" aria-label="Review list view">
        <DropdownMenuGroup>
          <DropdownMenuLabel>Show reviews</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            aria-label="Review grouping"
            value={value}
            onValueChange={(next) => {
              if (next === "repo" || next === "recent") onChange(next);
            }}
          >
            {groupModes.map((mode) => (
              <DropdownMenuRadioItem key={mode.value} value={mode.value}>
                {mode.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** Collapsed column: one mark per repository, unread dots, the current repository selected. */
function StreamRail({
  groups,
  currentStream,
  onOpenInbox,
}: {
  groups: ProjectGroup[];
  currentStream: string;
  onOpenInbox: () => void;
}) {
  const streams = useStreamNavState();
  return (
    <nav id="projects-nav" className="stream-rail" aria-label="Reviews">
      <IconButton
        id="reviews-expand"
        variant="rail"
        touch
        label="Expand reviews"
        onClick={() => streams.expand()}
      >
        <PanelLeftOpenIcon aria-hidden="true" />
      </IconButton>
      <IconButton
        id="inbox-open"
        variant="rail"
        touch
        label="Open inbox"
        aria-expanded={streams.inboxOpen}
        aria-controls="inbox"
        onClick={onOpenInbox}
      >
        <InboxIcon aria-hidden="true" />
      </IconButton>
      <Separator className="stream-rail-divider" />
      <ul className="stream-rail-list" aria-label="Repositories">
        {groups.map((group) => {
          const unread = group.streams.filter((stream) => stream.unseen).length;
          const current = group.streams.some(
            (stream) => stream.id === currentStream,
          );
          return (
            <li key={group.id}>
              <IconButton
                variant="rail"
                touch
                className="relative"
                label={
                  unread
                    ? `${group.name}, ${pluralize(unread, "new stream")}`
                    : group.name
                }
                aria-current={current ? "true" : undefined}
                onClick={() => streams.expand(group.id)}
              >
                {group.kind === "piped" ? (
                  <SquareTerminalIcon aria-hidden="true" />
                ) : (
                  <span className="text-xs font-semibold" aria-hidden="true">
                    {repoMonogram(group.name)}
                  </span>
                )}
                {unread > 0 && (
                  <span className="absolute top-1 right-1 inline-flex">
                    <StatusDot tone="accent" size="sm" />
                  </span>
                )}
              </IconButton>
            </li>
          );
        })}
      </ul>
    </nav>
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
        id={repoAnchor(group.id)}
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
          {group.streams.map(({ id, context, unseen: fresh }) => (
            <li key={id}>
              <StreamRow
                context={context}
                current={id === currentStream}
                fresh={fresh}
                meta={streamMeta(context, group.multipleHosts, now)}
                depth={1}
                onOpen={onOpen}
              />
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

/** Streams across repositories by activity; each row names its repository. */
function RecentList({
  groups,
  currentStream,
  now,
  onOpen,
}: {
  groups: ProjectGroup[];
  currentStream: string;
  now: number;
  onOpen: (event: MouseEvent<HTMLElement>, head: ApiContext) => void;
}) {
  const anchored = new Set<string>();
  return (
    <ul className="projects-list">
      {recentStreams(groups).map(({ id, context, unseen, group }) => {
        const first = !anchored.has(group.id);
        anchored.add(group.id);
        return (
          <li key={id}>
            <StreamRow
              id={first ? repoAnchor(group.id) : undefined}
              context={context}
              current={id === currentStream}
              fresh={unseen}
              repository={group.name}
              meta={streamMeta(context, group.multipleHosts, now)}
              onOpen={onOpen}
            />
          </li>
        );
      })}
    </ul>
  );
}

function StreamRow({
  id,
  context,
  current,
  fresh,
  repository,
  meta,
  depth,
  onOpen,
}: {
  id?: string | undefined;
  context: ApiContext;
  current: boolean;
  fresh: boolean;
  /** Labels rows outside a repository group. */
  repository?: string;
  meta: string;
  depth?: number;
  onOpen: (event: MouseEvent<HTMLElement>, head: ApiContext) => void;
}) {
  return (
    <ListRow
      id={id}
      {...(depth !== undefined && { depth })}
      className="flex-col items-stretch"
      render={<a href={streamPath(context)} />}
      aria-current={current ? "page" : undefined}
      title={`${repository ? `${repository} · ` : ""}${streamTitle(context)} · ${contextChangeLabel(context)}`}
      onClick={(event) => onOpen(event, context)}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {!contextIsPiped(context) && (
          <GitBranchIcon
            className={current ? undefined : "text-muted"}
            aria-hidden="true"
          />
        )}
        <span className="min-w-0 flex-1 truncate">{streamTitle(context)}</span>
        {fresh && !current && <NewMark />}
        {typeof context.changedFileCount === "number" && (
          <CountBadge
            count={context.changedFileCount}
            label={contextChangeLabel(context)}
          />
        )}
      </span>
      <span className="stream-meta truncate text-xs text-muted">
        {repository && (
          <span className="font-medium text-secondary">
            {repository}
            {meta && " · "}
          </span>
        )}
        {meta}
      </span>
    </ListRow>
  );
}

export function NewMark() {
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
          {collecting ? "Collecting reviews…" : "Loading reviews…"}
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
