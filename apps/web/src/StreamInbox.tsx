import type { ApiContext } from "@diffx/api";
import { GitBranchIcon, XIcon } from "lucide-react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { IconButton } from "@/components/icon-button";
import { ListRow } from "@/components/list-row";
import { PanelHeader } from "@/components/panel-header";
import { SearchField } from "@/components/search-field";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { contextActivity } from "./context-activity.ts";
import { pluralize } from "./format.ts";
import { MultiFilter } from "./PickerFilters.tsx";
import { contextChangeLabel, contextIsPiped } from "./project-picker.ts";
import {
  defaultStreamFilters,
  filterStreams,
  projectGroups,
  recentStreams,
  relativeAge,
  streamFacets,
  streamFiltersChanged,
  streamHost,
  streamKey,
  streamSessions,
  streamTitle,
  streamWorktree,
  type ProjectGroup,
  type ProjectStream,
} from "./project-nav.ts";
import { NewMark, streamClick, useNow } from "./ProjectsNav.tsx";
import { streamPath } from "./review-route.ts";
import { useCatalog } from "./session-context.tsx";
import { useStreamNavState } from "./stream-nav-context.tsx";

/**
 * The review column extended over the review island: every current stream
 * with facet filters. The review stays mounted and inert underneath.
 */
export function StreamInbox() {
  const { inboxOpen } = useStreamNavState();
  return inboxOpen ? <InboxOverlay /> : null;
}

function InboxOverlay() {
  const streams = useStreamNavState();
  const { filters, setFilters, closeInbox } = streams;
  const { heads, seen, current, openStream } = useCatalog();
  const now = useNow();
  const [query, setQuery] = useState("");
  const overlay = useRef<HTMLElement>(null);
  const search = useRef<HTMLInputElement>(null);
  // Choosing a review hands focus to the opened review instead of the opener.
  const restoreFocus = useRef(true);
  const groups = projectGroups(heads.contexts, seen);
  const facets = streamFacets(groups);
  const visible = filterStreams(groups, filters, query);
  const count = visible.reduce(
    (total, group) => total + group.streams.length,
    0,
  );
  const currentStream = current ? streamKey(current) : "";
  const filtered = streamFiltersChanged(filters) || query.trim() !== "";
  useEffect(() => {
    const element = overlay.current;
    const opener =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
    const covered = Array.from(element?.parentElement?.children ?? []).filter(
      (child): child is HTMLElement =>
        child instanceof HTMLElement &&
        child !== element &&
        !child.hasAttribute("inert"),
    );
    for (const child of covered) child.setAttribute("inert", "");
    search.current?.focus();
    return () => {
      for (const child of covered) child.removeAttribute("inert");
      if (!restoreFocus.current) return;
      (opener?.isConnected
        ? opener
        : document.querySelector<HTMLElement>("#inbox-open")
      )?.focus();
    };
  }, []);
  const choose = (context: ApiContext) => {
    restoreFocus.current = false;
    closeInbox();
    openStream(context);
  };
  const clear = () => {
    setFilters(defaultStreamFilters);
    setQuery("");
  };
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    // Facet menus render in a portal; their Escape closes only the menu.
    if (
      event.key === "Escape" &&
      event.target instanceof Node &&
      overlay.current?.contains(event.target)
    ) {
      event.preventDefault();
      closeInbox();
    }
  };
  const row = (
    stream: ProjectStream,
    group: ProjectGroup,
    labeled: boolean,
  ) => (
    <li key={stream.id}>
      <InboxRow
        stream={stream}
        repository={labeled ? group.name : undefined}
        piped={group.kind === "piped"}
        multipleHosts={group.multipleHosts}
        current={stream.id === currentStream}
        now={now}
        onChoose={choose}
      />
    </li>
  );
  return (
    <section
      id="inbox"
      ref={overlay}
      className="stream-inbox"
      aria-label="Inbox"
      onKeyDown={onKeyDown}
    >
      <PanelHeader
        title="Inbox"
        actions={
          <IconButton label="Close inbox" touch onClick={closeInbox}>
            <XIcon aria-hidden="true" />
          </IconButton>
        }
      />
      <div className="inbox-body">
        <div
          className="flex flex-wrap items-center gap-2"
          role="group"
          aria-label="Inbox filters"
        >
          <SearchField
            ref={search}
            label="Filter reviews"
            placeholder="Filter by repo, branch, worktree, session or host…"
            className="w-full"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          <MultiFilter
            label="Repository"
            options={facets.repos}
            selected={filters.repos}
            onChange={(repos) => setFilters({ ...filters, repos })}
          />
          {(facets.sessions.length > 0 || filters.sessions.length > 0) && (
            <MultiFilter
              label="Session"
              options={facets.sessions}
              selected={filters.sessions}
              onChange={(sessions) => setFilters({ ...filters, sessions })}
            />
          )}
          {(facets.worktrees.length > 0 || filters.worktrees.length > 0) && (
            <MultiFilter
              label="Worktree"
              options={facets.worktrees}
              selected={filters.worktrees}
              onChange={(worktrees) => setFilters({ ...filters, worktrees })}
            />
          )}
          {(facets.hosts.length > 1 || filters.hosts.length > 0) && (
            <MultiFilter
              label="Host"
              options={facets.hosts}
              selected={filters.hosts}
              onChange={(hosts) => setFilters({ ...filters, hosts })}
            />
          )}
          <label className="inline-flex min-h-control cursor-pointer items-center gap-1 text-sm text-secondary">
            <Checkbox
              checked={filters.unread}
              onCheckedChange={(unread) => setFilters({ ...filters, unread })}
            />
            Unread only
          </label>
          {filtered && (
            <Button type="button" variant="ghost" size="sm" onClick={clear}>
              Clear filters
            </Button>
          )}
        </div>
        <p className="visually-hidden" role="status" aria-live="polite">
          {filtered ? `${pluralize(count, "review")} shown` : ""}
        </p>
        {heads.error ? (
          <ErrorState message={heads.error} onRetry={heads.retry} />
        ) : !heads.loaded ? (
          <EmptyState role="status" description="Loading reviews…" />
        ) : !count ? (
          <EmptyState
            heading="p"
            title={groups.length ? "No matching reviews" : "No current changes"}
            description={
              groups.length
                ? "Change or clear the filters to see more reviews."
                : "Agent hooks and diffx sync publish changed checkouts here."
            }
            action={
              filtered && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={clear}
                >
                  Clear filters
                </Button>
              )
            }
          />
        ) : streams.group === "recent" ? (
          <ul className="inbox-list">
            {recentStreams(visible).map((stream) =>
              row(stream, stream.group, true),
            )}
          </ul>
        ) : (
          visible.map((group) => (
            <section key={group.id} aria-label={group.name}>
              <h3 className="m-0 flex items-center gap-1.5 px-1 pt-3 pb-1 text-xs font-semibold text-secondary">
                <ContextIcon piped={group.kind === "piped"} tone="muted" />
                {group.name}
              </h3>
              <ul className="inbox-list">
                {group.streams.map((stream) => row(stream, group, false))}
              </ul>
            </section>
          ))
        )}
      </div>
      <footer className="inbox-footer">
        {pluralize(count, "review")}
        {filtered &&
          ` of ${groups.reduce((total, group) => total + group.streams.length, 0)}`}
      </footer>
    </section>
  );
}

function InboxRow({
  stream: { context, unseen },
  repository,
  piped,
  multipleHosts,
  current,
  now,
  onChoose,
}: {
  stream: ProjectStream;
  /** Labels rows outside a repository section. */
  repository: string | undefined;
  piped: boolean;
  multipleHosts: boolean;
  current: boolean;
  now: number;
  onChoose: (context: ApiContext) => void;
}) {
  const details = [
    streamWorktree(context),
    ...streamSessions(context),
    multipleHosts ? streamHost(context) : "",
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <ListRow
      variant="option"
      className="flex-col items-stretch gap-0"
      render={<a href={streamPath(context)} />}
      aria-current={current ? "page" : undefined}
      onClick={(event) => streamClick(event, () => onChoose(context))}
    >
      <span className="flex min-w-0 items-center gap-1.5">
        {repository !== undefined && (
          <>
            <ContextIcon piped={piped} tone="muted" />
            <span className="max-w-1/2 shrink-0 truncate font-medium">
              {repository}
            </span>
            <span className="text-muted" aria-hidden="true">
              /
            </span>
          </>
        )}
        {!contextIsPiped(context) && (
          <GitBranchIcon className="text-muted" aria-hidden="true" />
        )}
        <span className="min-w-0 flex-1 truncate">{streamTitle(context)}</span>
        {unseen && !current && <NewMark />}
        {typeof context.changedFileCount === "number" && (
          <CountBadge
            count={context.changedFileCount}
            label={contextChangeLabel(context)}
          />
        )}
        <span className="w-8 shrink-0 text-right font-mono text-xs text-muted tabular-nums">
          {relativeAge(contextActivity(context), now)}
        </span>
      </span>
      {details && (
        <span className="truncate font-mono text-xs text-muted">{details}</span>
      )}
    </ListRow>
  );
}
