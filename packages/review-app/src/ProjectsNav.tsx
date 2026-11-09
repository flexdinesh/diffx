import { ProjectsSkeleton } from "./WorkspaceLoading.tsx";
import type { ApiContext } from "@diffx/api";
import {
  FolderGit2Icon,
  GitBranchIcon,
  GitMergeIcon,
  GitPullRequestClosedIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
  HistoryIcon,
  ListIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { BrandMark } from "@diffx/ui/components/brand-mark";
import { CollapsibleSection } from "@diffx/ui/components/collapsible-section";
import { ContextIcon } from "@diffx/ui/components/context-icon";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { DisclosureRow } from "@diffx/ui/components/disclosure-row";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { ErrorState } from "@diffx/ui/components/error-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { ListRow } from "@diffx/ui/components/list-row";
import { Notice } from "@diffx/ui/components/notice";
import { PanelHeader } from "@diffx/ui/components/panel-header";
import { Scrim } from "@diffx/ui/components/scrim";
import { StatusDot } from "@diffx/ui/components/status-dot";
import { Button } from "@diffx/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { Separator } from "@diffx/ui/components/ui/separator";
import { pluralize } from "@diffx/ui/lib/format";
import { save, saved } from "@diffx/ui/lib/storage";
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
  streamSections,
  streamTitle,
  type ProjectGroup,
  type PullRequestFilter,
  type StreamGroupMode,
} from "./project-nav.ts";
import { navigate } from "./identity-context.tsx";
import { useCatalog, useSession } from "./session-context.tsx";
import { streamPath } from "./review-route.ts";
import { settingsPath } from "./settings-layout.tsx";
import { useShellPolicy } from "./use-shell-policy.ts";
import { useSidebarState } from "./sidebar-context.tsx";
import { useStreamNavState } from "./stream-nav-context.tsx";

const groupModes = [
  { value: "repo", label: "Group by repo" },
  { value: "recent", label: "Recent first" },
] satisfies { value: StreamGroupMode; label: string }[];

const pullFilters = [
  { value: "open", label: "Open" },
  { value: "merged", label: "Merged" },
  { value: "all", label: "All" },
] satisfies { value: PullRequestFilter; label: string }[];

/**
 * The pull requests the review column, rail and header comboboxes list, or
 * null while the server has no GitHub integration and the column keeps one
 * list of current work.
 */
export function usePullRequestFilter(): PullRequestFilter | null {
  const { pullRequests } = useShellPolicy();
  const { pullFilter } = useStreamNavState();
  return pullRequests ? pullFilter : null;
}

/** Collapse keys for pull request repositories stay apart from machines'. */
function collapseKey(group: ProjectGroup, pullRequests: boolean) {
  return pullRequests ? `pr:${group.id}` : group.id;
}

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
        <BrandMark />
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
  const pulls = usePullRequestFilter();
  const [closed, setClosed] = useState(readCollapsedGroups);
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen, pulls ?? "open");
  const sections = pulls && streamSections(heads.contexts, seen, pulls);
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
  // A repository opens to its most active stream, and the group listing that
  // stream expands.
  const openLatest = (group: ProjectGroup) => {
    const latest = latestStream(group);
    if (!latest) return;
    const key = collapseKey(
      group,
      pulls !== null && pullRequestOf(latest.context) !== undefined,
    );
    if (closed.has(key)) {
      const next = new Set(closed);
      next.delete(key);
      updateClosed(next);
    }
    openStream(latest.context);
  };
  const list = (listed: ProjectGroup[], pullRequests: boolean) => (
    <StreamGroups
      groups={listed}
      mode={streams.group}
      closed={closed}
      pullRequests={pullRequests}
      onToggle={toggleGroup}
      onOpenLatest={openLatest}
      currentStream={currentStream}
      now={now}
      onOpen={open}
    />
  );
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
                pulls={pulls}
                onPullsChange={streams.setPullFilter}
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
          {!groups.length &&
          (!sections || heads.error || collecting || !heads.loaded) ? (
            <ProjectsEmpty
              loaded={heads.loaded}
              collecting={collecting}
              error={heads.error}
              onRetry={heads.retry}
            />
          ) : sections ? (
            <>
              <CollapsibleSection
                title="Pull requests"
                meta={
                  pullFilters.find((filter) => filter.value === pulls)?.label
                }
                storageKey="reviews-pull-requests-collapsed"
                leading
              >
                {sections.pullRequests.length ? (
                  list(sections.pullRequests, true)
                ) : (
                  <PullRequestsEmpty filter={pulls} />
                )}
              </CollapsibleSection>
              <CollapsibleSection
                title="From your machines"
                storageKey="reviews-machines-collapsed"
              >
                {sections.machines.length ? (
                  list(sections.machines, false)
                ) : (
                  <SectionNote>
                    Agent hooks and diffx sync publish changed checkouts here.
                  </SectionNote>
                )}
              </CollapsibleSection>
            </>
          ) : (
            list(groups, false)
          )}
        </div>
      </div>
    </>
  );
}

/** One section's repositories, grouped by repository or recent first. */
function StreamGroups({
  groups,
  mode,
  closed,
  pullRequests,
  onToggle,
  onOpenLatest,
  currentStream,
  now,
  onOpen,
}: {
  groups: ProjectGroup[];
  mode: StreamGroupMode;
  closed: Set<string>;
  pullRequests: boolean;
  onToggle: (key: string) => void;
  onOpenLatest: (group: ProjectGroup) => void;
  currentStream: string;
  now: number;
  onOpen: (event: MouseEvent<HTMLElement>, head: ApiContext) => void;
}) {
  if (mode === "recent")
    return (
      <RecentList
        groups={groups}
        currentStream={currentStream}
        now={now}
        onOpen={onOpen}
      />
    );
  return (
    <ul className="projects-list">
      {groups.map((group) => {
        const key = collapseKey(group, pullRequests);
        return (
          <ProjectItem
            key={group.id}
            group={group}
            listId={`project-${key}`}
            expanded={!closed.has(key)}
            onToggle={() => onToggle(key)}
            onOpenLatest={() => onOpenLatest(group)}
            currentStream={currentStream}
            now={now}
            onOpen={onOpen}
          />
        );
      })}
    </ul>
  );
}

/** Muted copy in place of a section's rows, indented like them. */
function SectionNote({ children }: { children: ReactNode }) {
  return <p className="m-0 px-4 py-1.5 text-xs text-muted">{children}</p>;
}

function PullRequestsEmpty({ filter }: { filter: PullRequestFilter }) {
  if (filter === "merged")
    return <SectionNote>No merged pull requests.</SectionNote>;
  if (filter === "all") return <SectionNote>No pull requests.</SectionNote>;
  const settings = settingsPath("github");
  return (
    <SectionNote>
      No open pull requests.{" "}
      <a
        href={settings}
        className="text-accent underline underline-offset-(--space-1)"
        onClick={(event) => {
          event.preventDefault();
          navigate(settings);
        }}
      >
        Follow repositories
      </a>{" "}
      in Settings.
    </SectionNote>
  );
}

function GroupModeMenu({
  value,
  onChange,
  pulls,
  onPullsChange,
}: {
  value: StreamGroupMode;
  onChange: (value: StreamGroupMode) => void;
  /** The pull request filter, shown only while the server has GitHub. */
  pulls: PullRequestFilter | null;
  onPullsChange: (value: PullRequestFilter) => void;
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
        {pulls && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>Pull requests</DropdownMenuLabel>
              <DropdownMenuRadioGroup
                aria-label="Pull request state"
                value={pulls}
                onValueChange={(next) => {
                  if (next === "open" || next === "merged" || next === "all")
                    onPullsChange(next);
                }}
              >
                {pullFilters.map((filter) => (
                  <DropdownMenuRadioItem
                    key={filter.value}
                    value={filter.value}
                  >
                    {filter.label}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
          </>
        )}
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
      <div className="stream-rail-head">
        <IconButton
          id="reviews-expand"
          variant="rail"
          touch
          label="Expand reviews"
          onClick={() => streams.expand()}
        >
          <PanelLeftOpenIcon aria-hidden="true" />
        </IconButton>
      </div>
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
  listId,
  expanded,
  onToggle,
  onOpenLatest,
  currentStream,
  now,
  onOpen,
}: {
  group: ProjectGroup;
  listId: string;
  expanded: boolean;
  onToggle: () => void;
  onOpenLatest: () => void;
  currentStream: string;
  now: number;
  onOpen: (event: MouseEvent<HTMLElement>, head: ApiContext) => void;
}) {
  const unseen = group.streams.some((stream) => stream.unseen);
  return (
    <li>
      <DisclosureRow
        depth={1}
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
      depth={(depth ?? 0) + 1}
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
  const [Icon, label] =
    pull.state === "merged"
      ? [GitMergeIcon, "Merged pull request"]
      : pull.state === "closed"
        ? [GitPullRequestClosedIcon, "Closed pull request"]
        : pull.draft
          ? [GitPullRequestDraftIcon, "Draft pull request"]
          : [GitPullRequestIcon, "Pull request"];
  return <Icon className={className} role="img" aria-label={label} />;
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
    <NewerNotice
      onView={newer && (() => openStream(newer, { restoreFocus: false }))}
    >
      {newer &&
        `Newer snapshot of ${streamTitle(newer)} · ${contextChangeLabel(newer)} · ${relativeAge(contextActivity(newer), now)}`}
    </NewerNotice>
  );
}

/**
 * Announces a newer snapshot of the open review without swapping it; stays
 * mounted (hidden) so its arrival is announced. `onView` opens the latest.
 */
export function NewerNotice({
  onView,
  children,
}: {
  onView: (() => void) | undefined;
  children: ReactNode;
}) {
  return (
    <Notice
      hidden={!onView}
      icon={onView && <HistoryIcon aria-hidden="true" />}
      action={
        onView && (
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={onView}
          >
            View latest
          </Button>
        )
      }
    >
      {onView && children}
    </Notice>
  );
}
