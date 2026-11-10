import {
  api,
  errorDetail,
  type ApiChangedFile,
  type ApiContext,
} from "@diffx/api";
import {
  BotIcon,
  CheckCheckIcon,
  CheckIcon,
  FolderGit2Icon,
  GitPullRequestIcon,
  HistoryIcon,
  InboxIcon,
  ListFilterIcon,
  MailIcon,
  PanelRightIcon,
  XIcon,
  type LucideIcon,
} from "lucide-react";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type ReactNode,
} from "react";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { ErrorState } from "@diffx/ui/components/error-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { KeyValue, KeyValueList } from "@diffx/ui/components/key-value-list";
import { LinkButton } from "@diffx/ui/components/link-button";
import { ListRow } from "@diffx/ui/components/list-row";
import { MarkdownText } from "@diffx/ui/components/markdown";
import { PanelHeader } from "@diffx/ui/components/panel-header";
import { SearchField } from "@diffx/ui/components/search-field";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { Kbd } from "@diffx/ui/components/ui/kbd";
import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import { Toggle } from "@diffx/ui/components/ui/toggle";
import { pluralize } from "@diffx/ui/lib/format";
import { save, saved } from "@diffx/ui/lib/storage";
import { saveBenchTrail } from "./bench-trail.ts";
import { historyRoute } from "./history.ts";
import { contextActivity } from "./context-activity.ts";
import { defaultScope } from "./diff-mode.ts";
import { navigate } from "./identity-context.tsx";
import { PaneResizer } from "./PaneResizer.tsx";
import {
  pullRequestOf,
  relativeAge,
  streamHarness,
  streamHost,
  streamKey,
  streamSessions,
  streamTitle,
  streamWorktree,
} from "./project-nav.ts";
import { contextChangeLabel } from "./project-picker.ts";
import { pullRequestStatus } from "./PullRequestBanner.tsx";
import {
  agentFacets,
  benchList,
  benchOrder,
  benchPath,
  benchQuery,
  benchRows,
  benchSections,
  benchStateLabels,
  benchViews,
  changedAreas,
  repositoryFacets,
  viewCounts,
  type BenchFacet,
  type BenchQuery,
  type BenchRow,
  type BenchView,
} from "./review-bench.ts";
import { streamPath } from "./review-route.ts";
import { ServerStatus } from "./ServerStatus.tsx";
import { useCatalog, useSessionShell } from "./session-context.tsx";
import { benchPeekWidth, useBenchPeekWidth } from "./use-bench-peek-width.ts";
import { settingsPath } from "./settings-layout.tsx";
import { SettingsButton } from "./SettingsButton.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { NewMark, StreamIcon, streamClick, useNow } from "./StreamParts.tsx";
import { SyncButton, Topbar } from "./Topbar.tsx";
import { useShellPolicy } from "./use-shell-policy.ts";

const viewIcons: Record<BenchView, LucideIcon> = {
  attention: InboxIcon,
  open: GitPullRequestIcon,
  closed: CheckIcon,
};

function typing(target: EventTarget | null) {
  return (
    target instanceof HTMLInputElement ||
    target instanceof HTMLTextAreaElement ||
    (target instanceof HTMLElement && target.isContentEditable)
  );
}

/** The bench's query mirrors the address: views and facets add history. */
function useBenchQuery() {
  const [query, setQueryState] = useState(() =>
    benchQuery(window.location.search),
  );
  useEffect(() => {
    // Leaving the bench through history must not rewrite its trail.
    const changed = () => {
      if (window.location.pathname === "/")
        setQueryState(benchQuery(window.location.search));
    };
    window.addEventListener("popstate", changed);
    return () => window.removeEventListener("popstate", changed);
  }, []);
  const setQuery = useCallback(
    (next: BenchQuery, options?: { replace?: boolean }) => {
      const path = benchPath(next);
      if (options?.replace) window.history.replaceState(null, "", path);
      else window.history.pushState(null, "", path);
      setQueryState(next);
    },
    [],
  );
  return [query, setQuery] as const;
}

/**
 * The Review bench at `/`: every review stream's head, what needs the reader,
 * and a peek before opening one. Facets lead, the table follows and the peek
 * trails on desktop; narrow screens list rows only.
 */
export function ReviewBench() {
  const { heads, marks, markViewed, markUnread, openStream, benchDefaultId } =
    useCatalog();
  const { collect, collecting, collectError } = useCatalog();
  const { collectOnLoad, remote, retry } = useSessionShell();
  const { serverStatus, pullRequests } = useShellPolicy();
  const { mobile } = useSidebarState();
  const now = useNow();
  const [query, setQuery] = useBenchQuery();
  const [peek, setPeekState] = useState(() => saved("bench-peek") !== "false");
  const setPeek = (open: boolean) => {
    save("bench-peek", String(open));
    setPeekState(open);
  };
  const rows = useMemo(
    () => benchRows(heads.contexts, marks),
    [heads.contexts, marks],
  );
  const list = useMemo(() => benchList(rows, query), [rows, query]);
  const sections = useMemo(
    () => benchSections(list, query.view),
    [list, query.view],
  );
  const order = useMemo(() => benchOrder(sections), [sections]);
  const counts = viewCounts(rows);
  const [chosen, setChosen] = useState("");
  const launched = heads.contexts.find(
    (context) => context.id === benchDefaultId,
  );
  // The selection survives reordering; it falls back to the launched
  // checkout, then the first row.
  const selected =
    order.find((row) => row.id === chosen) ??
    order.find((row) => launched && row.id === streamKey(launched)) ??
    order[0];
  const rowRefs = useRef(new Map<string, HTMLAnchorElement>());
  const search = useRef<HTMLInputElement>(null);
  const path = benchPath(query);
  const orderKey = order.map((row) => row.id).join("\n");
  useEffect(() => {
    saveBenchTrail({ path, order: orderKey ? orderKey.split("\n") : [] });
  }, [path, orderKey]);
  // Keyboard review starts on the list: the selected row takes focus once
  // rows load, unless the reader already focused something else.
  const focused = useRef(false);
  const selectedId = selected?.id;
  useEffect(() => {
    if (focused.current || !selectedId) return;
    focused.current = true;
    if (document.activeElement === document.body)
      rowRefs.current.get(selectedId)?.focus({ preventScroll: true });
  }, [selectedId]);
  const focusRow = (row: BenchRow | undefined) => {
    if (!row) return;
    setChosen(row.id);
    rowRefs.current.get(row.id)?.focus();
  };
  const toggleViewed = (row: BenchRow) => {
    if (row.state === "viewed") markUnread(row.context);
    else markViewed([row.context]);
  };
  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (!event.altKey || event.metaKey || event.ctrlKey) return;
      if (!event.shiftKey && event.code === "Slash") {
        event.preventDefault();
        search.current?.focus();
        return;
      }
      if (!event.shiftKey || typing(event.target) || !selected) return;
      if (event.code === "KeyV") {
        event.preventDefault();
        markViewed([selected.context]);
      } else if (event.code === "KeyU") {
        event.preventDefault();
        markUnread(selected.context);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [selected, markViewed, markUnread]);
  const onListKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (typing(event.target) || event.altKey || event.metaKey || event.ctrlKey)
      return;
    const index = selected ? order.indexOf(selected) : -1;
    const move = {
      ArrowDown: order[Math.min(order.length - 1, index + 1)],
      ArrowUp: order[Math.max(0, index - 1)],
      Home: order[0],
      End: order[order.length - 1],
    }[event.key];
    if (move) {
      event.preventDefault();
      focusRow(move);
    } else if (event.key === " " && !mobile) {
      event.preventDefault();
      setPeek(!peek);
    } else if (event.key === "Escape" && peek && !mobile) {
      setPeek(false);
    }
  };
  const filtered = Boolean(query.repo || query.agent || query.q);
  const showPeek = peek && !mobile;
  return (
    <>
      <Topbar
        identity={
          <h1 id="bench-title" className="bench-title">
            <InboxIcon aria-hidden="true" />
            Reviews
          </h1>
        }
        actions={
          <>
            {pullRequests && <HistoryLink />}
            {collect && (
              <SyncButton
                busy={collecting}
                onClick={() => {
                  if (!collecting) void collect();
                }}
              />
            )}
          </>
        }
      />
      <div className="bench" data-peek={showPeek || undefined}>
        {!mobile && (
          <BenchFacets
            query={query}
            rows={rows}
            counts={counts}
            onChange={setQuery}
          />
        )}
        <main className="bench-main" aria-labelledby="bench-title">
          <PanelHeader className="bench-toolbar">
            {mobile && (
              <ViewMenu query={query} counts={counts} onChange={setQuery} />
            )}
            <SearchField
              ref={search}
              className="bench-search"
              label="Filter reviews"
              placeholder="Filter reviews…"
              shortcut={{ alt: true, key: "/" }}
              value={query.q}
              onChange={(event) =>
                setQuery({ ...query, q: event.target.value }, { replace: true })
              }
              onKeyDown={(event) => {
                if (event.key === "ArrowDown") {
                  event.preventDefault();
                  focusRow(selected ?? order[0]);
                }
              }}
            />
            {filtered && (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() =>
                  setQuery({ view: query.view, repo: "", agent: "", q: "" })
                }
              >
                Clear filters
              </Button>
            )}
            {!mobile && (
              <Toggle
                className="ml-auto"
                pressed={peek}
                onPressedChange={setPeek}
                aria-label="Peek"
              >
                <PanelRightIcon aria-hidden="true" />
                Peek
                <Kbd>Space</Kbd>
              </Toggle>
            )}
          </PanelHeader>
          <div
            className="bench-scroll"
            onKeyDown={onListKeyDown}
            aria-busy={!heads.loaded || (collectOnLoad && !rows.length)}
          >
            {collectError && (
              <div className="p-4 pb-0">
                <ErrorState message={collectError} onRetry={retry} />
              </div>
            )}
            {heads.error && !rows.length ? (
              <div className="p-4">
                <ErrorState message={heads.error} onRetry={heads.retry} />
              </div>
            ) : !heads.loaded || (collectOnLoad && !rows.length) ? (
              <BenchSkeleton
                label={
                  collectOnLoad ? "Collecting reviews…" : "Loading reviews…"
                }
              />
            ) : !list.length ? (
              <BenchEmpty
                query={query}
                filtered={filtered}
                anything={rows.length > 0}
                stored={heads.contexts.length > 0}
                remote={remote}
                onChange={setQuery}
              />
            ) : (
              <>
                <div className="bench-row bench-columns" aria-hidden="true">
                  <span />
                  <span />
                  <span>Review</span>
                  <span>Repository</span>
                  <span className="bench-source">Source</span>
                  <span>State</span>
                  <span className="text-right">Files</span>
                  <span className="text-right">Activity</span>
                </div>
                {sections.map((section) => (
                  <section
                    key={section.id}
                    aria-labelledby={`bench-section-${section.id}`}
                  >
                    <div className="bench-section">
                      <h2 id={`bench-section-${section.id}`}>
                        {section.title}
                      </h2>
                      <CountBadge count={section.rows.length} />
                      {section.id === "unread" && (
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          className="ml-auto"
                          onClick={() =>
                            markViewed(section.rows.map((row) => row.context))
                          }
                        >
                          <CheckCheckIcon aria-hidden="true" />
                          Mark all viewed
                        </Button>
                      )}
                    </div>
                    <ul className="bench-list" aria-label={section.title}>
                      {section.rows.map((row) => (
                        <li key={row.id}>
                          <BenchRowLink
                            row={row}
                            now={now}
                            selected={row === selected}
                            previews={showPeek}
                            linkRef={(element) => {
                              if (element) rowRefs.current.set(row.id, element);
                              else rowRefs.current.delete(row.id);
                            }}
                            onSelect={() => setChosen(row.id)}
                            onOpen={() => openStream(row.context)}
                          />
                        </li>
                      ))}
                    </ul>
                  </section>
                ))}
                {pullRequests &&
                  query.view !== "closed" &&
                  !rows.some((row) => pullRequestOf(row.context)) && (
                    <FollowNote />
                  )}
              </>
            )}
          </div>
          <footer className="main-footer">
            <span className="footer-shortcuts">
              <kbd>↑</kbd> <kbd>↓</kbd> move <kbd>Enter</kbd> open{" "}
              <kbd>Space</kbd> peek <kbd>Alt+Shift+V</kbd> viewed
            </span>
            <span className="footer-details">
              <span>{pluralize(list.length, "review")}</span>
              <span className="flex items-center gap-1">
                {serverStatus && <ServerStatus />}
                <SettingsButton />
              </span>
            </span>
          </footer>
        </main>
        {showPeek && (
          <BenchPeek
            row={selected}
            now={now}
            onOpen={(context) => openStream(context)}
            onToggleViewed={toggleViewed}
            onClose={() => setPeek(false)}
          />
        )}
      </div>
    </>
  );
}

function BenchFacets({
  query,
  rows,
  counts,
  onChange,
}: {
  query: BenchQuery;
  rows: BenchRow[];
  counts: Record<BenchView, number>;
  onChange: (query: BenchQuery) => void;
}) {
  const repositories = repositoryFacets(rows, query.view);
  const agents = agentFacets(rows, query.view);
  return (
    <nav className="bench-facets" aria-label="Review views">
      <PanelHeader title="Views" />
      <div className="bench-facets-scroll">
        <ul className="bench-list">
          {benchViews.map((view) => (
            <li key={view.value}>
              <FacetLink
                href={benchPath({ ...query, view: view.value })}
                current={query.view === view.value}
                Icon={viewIcons[view.value]}
                label={view.label}
                count={counts[view.value]}
                onSelect={() => onChange({ ...query, view: view.value })}
              />
            </li>
          ))}
        </ul>
        <FacetGroup
          title="Repositories"
          Icon={FolderGit2Icon}
          facets={repositories}
          value={query.repo}
          href={(repo) => benchPath({ ...query, repo })}
          onSelect={(repo) => onChange({ ...query, repo })}
        />
        {agents.length > 0 && (
          <FacetGroup
            title="Agents"
            Icon={BotIcon}
            facets={agents}
            value={query.agent}
            href={(agent) => benchPath({ ...query, agent })}
            onSelect={(agent) => onChange({ ...query, agent })}
          />
        )}
      </div>
    </nav>
  );
}

/** A facet filters on click and clears when clicked again. */
function FacetGroup({
  title,
  Icon,
  facets,
  value,
  href,
  onSelect,
}: {
  title: string;
  Icon: LucideIcon;
  facets: BenchFacet[];
  value: string;
  href: (value: string) => string;
  onSelect: (value: string) => void;
}) {
  if (!facets.length) return null;
  return (
    <>
      <h2 className="bench-facet-title">{title}</h2>
      <ul className="bench-list" aria-label={title}>
        {facets.map((facet) => {
          const current = facet.value === value;
          const next = current ? "" : facet.value;
          return (
            <li key={facet.value}>
              <FacetLink
                href={href(next)}
                current={current}
                Icon={Icon}
                label={facet.label}
                count={facet.count}
                onSelect={() => onSelect(next)}
              />
            </li>
          );
        })}
      </ul>
    </>
  );
}

export function FacetLink({
  href,
  current,
  Icon,
  label,
  count,
  onSelect,
}: {
  href: string;
  current: boolean;
  Icon: LucideIcon;
  label: string;
  count: number;
  onSelect: () => void;
}) {
  return (
    <ListRow
      depth={1}
      render={<a href={href} />}
      aria-current={current ? "true" : undefined}
      onClick={(event) => streamClick(event, onSelect)}
    >
      <Icon aria-hidden="true" />
      <span className="min-w-0 flex-1 truncate">{label}</span>
      <CountBadge count={count} />
    </ListRow>
  );
}

/** Narrow screens choose a view from a menu in place of the facet pane. */
function ViewMenu({
  query,
  counts,
  onChange,
}: {
  query: BenchQuery;
  counts: Record<BenchView, number>;
  onChange: (query: BenchQuery) => void;
}) {
  const current = benchViews.find((view) => view.value === query.view);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button type="button" variant="ghost" size="sm" touch>
            <ListFilterIcon aria-hidden="true" />
            {current?.label}
          </Button>
        }
      />
      <DropdownMenuContent align="start" aria-label="Review views">
        <DropdownMenuRadioGroup
          aria-label="Review view"
          value={query.view}
          onValueChange={(next) => {
            const view = benchViews.find((item) => item.value === next);
            if (view) onChange({ ...query, view: view.value });
          }}
        >
          {benchViews.map((view) => (
            <DropdownMenuRadioItem
              key={view.value}
              value={view.value}
              closeOnClick
            >
              {view.label} ({counts[view.value]})
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function benchSource(context: ApiContext) {
  const pull = pullRequestOf(context);
  if (pull) return `GitHub · @${pull.author.login}`;
  return [streamHarness(context), streamHost(context)]
    .filter(Boolean)
    .join(" · ");
}

/**
 * With the peek showing, a click selects the row and previews it; a double
 * click or Enter (a click without a pointer, detail 0) opens it. Without the
 * peek a click opens. Modified clicks keep the link's new-tab behavior.
 */
function BenchRowLink({
  row,
  now,
  selected,
  previews,
  linkRef,
  onSelect,
  onOpen,
}: {
  row: BenchRow;
  now: number;
  selected: boolean;
  previews: boolean;
  linkRef: (element: HTMLAnchorElement | null) => void;
  onSelect: () => void;
  onOpen: () => void;
}) {
  const { context, state, unread } = row;
  const title = streamTitle(context);
  const source = benchSource(context);
  return (
    <ListRow
      variant="table"
      className="bench-row"
      render={<a ref={linkRef} href={streamPath(context)} />}
      aria-current={selected ? "true" : undefined}
      tabIndex={selected ? 0 : -1}
      title={`${context.name} · ${title} · ${contextChangeLabel(context)}`}
      onFocus={onSelect}
      onClick={(event) => {
        onSelect();
        const opens = !previews || event.detail === 0 || event.detail >= 2;
        streamClick(event, opens ? onOpen : () => undefined);
      }}
    >
      <span className="flex justify-center">{unread && <NewMark />}</span>
      <StreamIcon context={context} muted={!selected} />
      <span
        className={`min-w-0 truncate${unread ? " font-semibold" : ""}${pullRequestOf(context) ? "" : " font-mono"}`}
      >
        {title}
      </span>
      <span className="min-w-0 truncate font-mono text-xs text-secondary">
        {context.name}
      </span>
      <span className="bench-source min-w-0 truncate text-xs text-muted">
        {source}
      </span>
      <span
        className={`text-xs ${unread ? "text-fg" : "text-muted"}`}
        aria-label={`State: ${benchStateLabels[state]}`}
      >
        {benchStateLabels[state]}
      </span>
      <span className="text-right">
        {typeof context.changedFileCount === "number" && (
          <CountBadge
            count={context.changedFileCount}
            label={contextChangeLabel(context)}
          />
        )}
      </span>
      <span className="text-right font-mono text-xs text-muted tabular-nums">
        {relativeAge(contextActivity(context), now)}
      </span>
    </ListRow>
  );
}

/** Opens History: the pull requests the reader opened and merged. */
function HistoryLink() {
  return (
    <LinkButton
      variant="shell"
      size="sm"
      touch
      href={historyRoute}
      aria-label="History"
      title="Pull requests you opened and merged"
      onClick={(event) => streamClick(event, () => navigate(historyRoute))}
    >
      <HistoryIcon aria-hidden="true" />
      <span className="max-md:hidden">History</span>
    </LinkButton>
  );
}

function BenchSkeleton({ label }: { label: string }) {
  return (
    <div role="status" className="flex flex-col gap-3 p-4">
      {Array.from({ length: 6 }, (_, index) => (
        <Skeleton key={index} shape="control" className="w-full" />
      ))}
      <p className="visually-hidden">{label}</p>
    </div>
  );
}

function FollowNote() {
  const settings = settingsPath("github");
  return (
    <p className="m-0 px-4 py-3 text-xs text-muted">
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
      in Settings to review their pull requests here.
    </p>
  );
}

function BenchEmpty({
  query,
  filtered,
  anything,
  stored,
  remote,
  onChange,
}: {
  query: BenchQuery;
  filtered: boolean;
  anything: boolean;
  /** Stored heads exist, though none has changes. */
  stored: boolean;
  remote: boolean;
  onChange: (query: BenchQuery) => void;
}) {
  const all = { view: query.view, repo: "", agent: "", q: "" };
  let title: ReactNode;
  let description: ReactNode;
  let action: ReactNode;
  if (!anything && stored) {
    title = "No changes to review";
    description =
      "Snapshots without changes stay reachable through Search reviews.";
  } else if (!anything) {
    title = "No review contexts yet";
    description = remote
      ? "Create a collector token in settings, then run diffx sync to publish your changes."
      : "Run diffx to review a checkout, or diffx sync to publish remotely.";
    action = remote && (
      <Button
        type="button"
        variant="outline"
        onClick={() => navigate("/settings")}
      >
        Create a collector token
      </Button>
    );
  } else if (filtered) {
    title = "No matching reviews";
    description = "No review in this view matches the filters.";
    action = (
      <Button type="button" variant="outline" onClick={() => onChange(all)}>
        Clear filters
      </Button>
    );
  } else if (query.view === "attention") {
    title = "You're caught up";
    description = "Nothing new or updated since you last looked.";
    action = (
      <Button
        type="button"
        variant="outline"
        onClick={() => onChange({ ...all, view: "open" })}
      >
        Show all open
      </Button>
    );
  } else if (query.view === "open") {
    title = "No open reviews";
    description = "Agent hooks and diffx sync publish changed checkouts here.";
  } else {
    title = "No closed reviews";
    description =
      "Merged and closed pull requests and stale branches appear here.";
  }
  return (
    <EmptyState
      size="page"
      heading="h2"
      role="status"
      title={title}
      description={description}
      action={action}
    />
  );
}

type PeekData =
  | { status: "loading" }
  | { status: "error"; detail: string }
  | {
      status: "ready";
      context: ApiContext;
      files: ApiChangedFile[];
      reviewed: number | null;
    };

/** Detail, current diff manifest and reviewed marks for one head. */
function usePeekData(context: ApiContext | undefined, marks: boolean) {
  const [data, setData] = useState<{ id: string; peek: PeekData }>({
    id: "",
    peek: { status: "loading" },
  });
  const id = context?.id ?? "";
  useEffect(() => {
    if (!context) return;
    const controller = new AbortController();
    const contextId = context.id;
    const scope = defaultScope(context);
    const signal = controller.signal;
    const path = { contextId };
    void Promise.all([
      api.GET("/api/v2/contexts/{contextId}", {
        params: { path },
        signal,
      }),
      api.GET("/api/v2/contexts/{contextId}/diffs/current", {
        params: { path, query: { scope } },
        signal,
      }),
      marks
        ? api.GET("/api/v2/contexts/{contextId}/review-marks", {
            params: { path, query: { scope } },
            signal,
          })
        : Promise.resolve(null),
    ])
      .then(([detail, diff, reviewed]) => {
        if (signal.aborted) return;
        if (!detail.data || !diff.data) {
          setData({
            id: contextId,
            peek: {
              status: "error",
              detail: errorDetail(
                detail.error ?? diff.error,
                "Unable to load this review",
              ),
            },
          });
          return;
        }
        const files = diff.data.files;
        const fingerprints = new Map(
          (reviewed?.data?.marks ?? []).map((mark) => [
            mark.fileId,
            mark.fileVersion,
          ]),
        );
        setData({
          id: contextId,
          peek: {
            status: "ready",
            context: detail.data,
            files,
            reviewed: reviewed?.data
              ? files.filter(
                  (file) => fingerprints.get(file.id) === file.fingerprint,
                ).length
              : null,
          },
        });
      })
      .catch((error: unknown) => {
        if (!signal.aborted)
          setData({
            id: contextId,
            peek: {
              status: "error",
              detail: errorDetail(error, "Unable to load this review"),
            },
          });
      });
    return () => controller.abort();
    // The head's identity selects the request; its object changes on reloads.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, marks]);
  return data.id === id ? data.peek : { status: "loading" as const };
}

/** What the selected review holds, before opening it. Peek never marks it. */
function BenchPeek({
  row,
  now,
  onOpen,
  onToggleViewed,
  onClose,
}: {
  row: BenchRow | undefined;
  now: number;
  onOpen: (context: ApiContext) => void;
  onToggleViewed: (row: BenchRow) => void;
  onClose: () => void;
}) {
  const { reviewMarks } = useShellPolicy();
  const peek = usePeekData(row?.context, reviewMarks);
  const { width, setWidth, reset } = useBenchPeekWidth();
  return (
    <aside className="bench-peek" aria-label="Peek">
      <PaneResizer
        label="Resize peek"
        min={benchPeekWidth.min}
        max={benchPeekWidth.max}
        width={width}
        edge="start"
        onResize={setWidth}
        onReset={reset}
      />
      <PanelHeader
        title="Peek"
        actions={
          <IconButton label="Close peek" touch onClick={onClose}>
            <XIcon aria-hidden="true" />
          </IconButton>
        }
      />
      <div className="bench-peek-scroll">
        {!row ? (
          <EmptyState title="Nothing selected" />
        ) : (
          <PeekBody
            row={row}
            peek={peek}
            now={now}
            onOpen={onOpen}
            onToggleViewed={onToggleViewed}
          />
        )}
      </div>
    </aside>
  );
}

function PeekBody({
  row,
  peek,
  now,
  onOpen,
  onToggleViewed,
}: {
  row: BenchRow;
  peek: PeekData;
  now: number;
  onOpen: (context: ApiContext) => void;
  onToggleViewed: (row: BenchRow) => void;
}) {
  const context = peek.status === "ready" ? peek.context : row.context;
  const pull = pullRequestOf(context);
  const status = pull && pullRequestStatus(pull);
  const sessions = streamSessions(context);
  const worktree = streamWorktree(context);
  const files = peek.status === "ready" ? peek.files : [];
  const additions = files.reduce((total, file) => total + file.additions, 0);
  const deletions = files.reduce((total, file) => total + file.deletions, 0);
  const viewed = row.state === "viewed";
  return (
    <div className="flex flex-col gap-4 p-4">
      <div className="flex flex-col gap-2">
        <p className="m-0 flex items-center gap-1.5 text-xs text-muted">
          <StreamIcon context={context} muted />
          <span className="font-mono">{context.name}</span>
          <span aria-hidden="true">·</span>
          <span>{pull ? "GitHub pull request" : "From your machines"}</span>
        </p>
        <h2
          className={`m-0 text-md leading-ui font-semibold wrap-anywhere${pull ? "" : " font-mono"}`}
        >
          {streamTitle(context)}
        </h2>
        <p className="m-0 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-secondary">
          {status && (
            <Badge variant="status" tone={status.tone}>
              {status.label}
            </Badge>
          )}
          <Badge variant="status" tone={row.unread ? "accent" : "neutral"}>
            {benchStateLabels[row.state]}
          </Badge>
          {pull ? (
            <>
              <span>@{pull.author.login}</span>
              <span className="font-mono text-xs text-muted">
                {pull.headRef} → {pull.baseRef}
              </span>
            </>
          ) : (
            <span>{benchSource(context)}</span>
          )}
          <span className="font-mono text-xs text-muted tabular-nums">
            {relativeAge(contextActivity(context), now)}
          </span>
        </p>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="button" onClick={() => onOpen(row.context)}>
          Open review
          <Kbd>Enter</Kbd>
        </Button>
        <Button
          type="button"
          variant="outline-muted"
          onClick={() => onToggleViewed(row)}
        >
          {viewed ? (
            <MailIcon aria-hidden="true" />
          ) : (
            <CheckCheckIcon aria-hidden="true" />
          )}
          {viewed ? "Mark unread" : "Mark viewed"}
        </Button>
        {pull && (
          <LinkButton variant="outline-muted" href={pull.url} external>
            Open on GitHub
          </LinkButton>
        )}
      </div>
      <PeekSection title="Changes">
        <KeyValueList>
          <KeyValue label="Changed files">
            {contextChangeLabel(context)}
            {peek.status === "ready" && (
              <span className="ml-2 font-mono text-xs">
                <span className="text-success">+{additions}</span>{" "}
                <span className="text-error">−{deletions}</span>
              </span>
            )}
          </KeyValue>
          {context.commits && (
            <KeyValue label="Commits" font="mono">
              {context.commits.count}
            </KeyValue>
          )}
          {worktree && (
            <KeyValue label="Worktree" font="mono">
              {worktree}
            </KeyValue>
          )}
          {peek.status === "ready" && peek.reviewed !== null && (
            <KeyValue label="Your progress">
              {peek.reviewed} of {pluralize(files.length, "file")} reviewed
            </KeyValue>
          )}
        </KeyValueList>
      </PeekSection>
      {pull?.body.trim() && (
        <PeekSection title="Description">
          <div className="bench-peek-description">
            <MarkdownText>{pull.body}</MarkdownText>
          </div>
        </PeekSection>
      )}
      {sessions.length > 0 && (
        <PeekSection title="Agent sessions">
          <ul className="m-0 flex list-none flex-col gap-1 p-0 text-sm text-secondary">
            {sessions.map((session) => (
              <li key={session}>{session}</li>
            ))}
          </ul>
        </PeekSection>
      )}
      <PeekSection title="Where it changes">
        {peek.status === "error" ? (
          <p className="m-0 text-sm text-error">{peek.detail}</p>
        ) : peek.status === "loading" ? (
          <div role="status" className="flex flex-col gap-2">
            <Skeleton shape="label" className="w-full" />
            <Skeleton shape="label" className="w-2/3" />
            <span className="visually-hidden">Loading changes…</span>
          </div>
        ) : (
          <KeyValueList>
            {changedAreas(files.map((file) => file.path)).map(
              ({ area, count }) => (
                <KeyValue key={area} label={area} font="mono">
                  {pluralize(count, "file")}
                </KeyValue>
              ),
            )}
          </KeyValueList>
        )}
      </PeekSection>
    </div>
  );
}

function PeekSection({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="m-0 text-xs leading-ui font-semibold text-secondary">
        {title}
      </h3>
      {children}
    </section>
  );
}
