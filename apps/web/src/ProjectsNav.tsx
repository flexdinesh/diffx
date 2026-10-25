import { ProjectsSkeleton } from "./WorkspaceLoading.tsx";
import type { ApiContext } from "@diffx/api";
import {
  FolderGit2Icon,
  GitBranchIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
  HistoryIcon,
  ListIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { DisclosureRow } from "@/components/disclosure-row";
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
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Separator } from "@/components/ui/separator";
import { pluralize } from "./format.ts";
import { save, saved } from "./preferences.ts";
import { contextChangeLabel } from "./project-picker.ts";
import { contextActivity } from "./context-activity.ts";
import {
  latestStream,
  newerHead,
  projectGroups,
  pullRequestOf,
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
  const { heads, current, openStream, seen, collecting } = useCatalog();
  const [closed, setClosed] = useState(readCollapsedGroups);
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen);
  const currentStream = current ? streamKey(current) : "";
  const drawerOpen = sidebar.drawer === "reviews";
  const rail = !sidebar.mobile && streams.state === "rail";
  // Saved before navigating: opening a stream can remount the column (the
  // loading shell renders its own), which would drop a pending save effect.
  const updateClosed = (next: Set<string>) => {
    save("projects-closed-groups", JSON.stringify([...next]));
    setClosed(next);
  };
  const toggleGroup = (id: string) => {
    const next = new Set(closed);
    if (!next.delete(id)) next.add(id);
    updateClosed(next);
  };
  const open = (event: MouseEvent<HTMLElement>, head: ApiContext) =>
    streamClick(event, () => openStream(head));
  // A repository opens to its most active stream, and its group expands.
  const openLatest = (group: ProjectGroup) => {
    const latest = latestStream(group);
    if (!latest) return;
    if (closed.has(group.id)) {
      const next = new Set(closed);
      next.delete(group.id);
      updateClosed(next);
    }
    openStream(latest.context);
  };
  if (rail)
    return (
      <StreamRail
        groups={groups}
        currentStream={currentStream}
        onOpenLatest={openLatest}
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
                  onOpenLatest={() => openLatest(group)}
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

/**
 * Collapsed column: one mark per repository, unread dots, the current
 * repository selected. A mark opens that repository's latest stream.
 */
function StreamRail({
  groups,
  currentStream,
  onOpenLatest,
}: {
  groups: ProjectGroup[];
  currentStream: string;
  onOpenLatest: (group: ProjectGroup) => void;
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
                    ? `Open latest in ${group.name}, ${pluralize(unread, "new stream")}`
                    : `Open latest in ${group.name}`
                }
                aria-current={current ? "true" : undefined}
                onClick={() => onOpenLatest(group)}
              >
                <span className="text-xs font-semibold" aria-hidden="true">
                  {repoMonogram(group.name)}
                </span>
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
  onOpenLatest,
  currentStream,
  now,
  onOpen,
}: {
  group: ProjectGroup;
  expanded: boolean;
  onToggle: () => void;
  onOpenLatest: () => void;
  currentStream: string;
  now: number;
  onOpen: (event: MouseEvent<HTMLElement>, head: ApiContext) => void;
}) {
  const unseen = group.streams.some((stream) => stream.unseen);
  const listId = `project-${group.id}`;
  return (
    <li>
      <DisclosureRow
        className="mt-1"
        open={expanded}
        onToggle={onToggle}
        toggleLabel={`${expanded ? "Collapse" : "Expand"} ${group.name}`}
        controls={listId}
        onActivate={onOpenLatest}
        label={
          unseen && !expanded
            ? `Open latest in ${group.name}, new changes`
            : `Open latest in ${group.name}`
        }
      >
        <ContextIcon tone="muted" />
        <span className="min-w-0 flex-1 truncate font-medium">
          {group.name}
        </span>
        {unseen && !expanded && <NewMark />}
        <span className="shrink-0 font-mono text-xs text-muted tabular-nums">
          {relativeAge(group.lastActivity, now)}
        </span>
      </DisclosureRow>
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
  return (
    <ul className="projects-list">
      {recentStreams(groups).map(({ id, context, unseen, group }) => (
        <li key={id}>
          <StreamRow
            context={context}
            current={id === currentStream}
            fresh={unseen}
            repository={group.name}
            meta={streamMeta(context, group.multipleHosts, now)}
            onOpen={onOpen}
          />
        </li>
      ))}
    </ul>
  );
}

function StreamRow({
  context,
  current,
  fresh,
  repository,
  meta,
  depth,
  onOpen,
}: {
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
      {...(depth !== undefined && { depth })}
      className="flex-col items-stretch"
      render={<a href={streamPath(context)} />}
      aria-current={current ? "page" : undefined}
      title={`${repository ? `${repository} · ` : ""}${streamTitle(context)} · ${contextChangeLabel(context)}`}
      onClick={(event) => onOpen(event, context)}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        <StreamIcon context={context} muted={!current} />
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

/** Pull requests carry their GitHub state; other streams show a branch. */
export function StreamIcon({
  context,
  muted = false,
}: {
  context: ApiContext;
  muted?: boolean;
}) {
  const pull = pullRequestOf(context);
  const className = muted ? "text-muted" : undefined;
  if (!pull) return <GitBranchIcon className={className} aria-hidden="true" />;
  const Icon = pull.draft ? GitPullRequestDraftIcon : GitPullRequestIcon;
  return (
    <Icon
      className={className}
      role="img"
      aria-label={pull.draft ? "Draft pull request" : "Pull request"}
    />
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
