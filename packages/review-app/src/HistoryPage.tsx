import {
  CalendarIcon,
  ChartColumnIcon,
  CheckIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  CopyIcon,
  FolderGit2Icon,
  GitMergeIcon,
  GitPullRequestArrowIcon,
  HistoryIcon,
  ListFilterIcon,
  ListIcon,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { ErrorState } from "@diffx/ui/components/error-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { LinkButton } from "@diffx/ui/components/link-button";
import { ListRow } from "@diffx/ui/components/list-row";
import { PanelHeader } from "@diffx/ui/components/panel-header";
import { SegmentedControl } from "@diffx/ui/components/segmented-control";
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
import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import { formatCount, pluralize } from "@diffx/ui/lib/format";
import { AccountButton } from "./account.tsx";
import { benchTrail } from "./bench-trail.ts";
import { CopyDialog } from "./CopyDialog.tsx";
import {
  canStepBack,
  chartUnit,
  clearFilters,
  eventClock,
  eventLabel,
  formatDuration,
  historyDays,
  historyEdges,
  historyFiltered,
  historyPath,
  historyRecap,
  historyWindow,
  listSummary,
  listWindow,
  openToMerge,
  pullState,
  rangeChoices,
  recapFilters,
  retentionDays,
  retentionNote,
  stateLabels,
  stepHistory,
  windowLabel,
  windowPhrase,
  type HistoryActivity,
  type HistoryData,
  type HistoryPullRequest,
  type HistoryPullState,
  type HistoryQuery,
  type HistoryRangeOption,
  type HistoryView,
  type HistoryWindow,
} from "./history.ts";
import {
  HistoryOverview,
  HistoryOverviewSkeleton,
} from "./HistoryOverview.tsx";
import { HistoryEmpty, PullIcon, pullStateIcons } from "./HistoryParts.tsx";
import { navigate } from "./identity-context.tsx";
import { FacetLink } from "./ReviewBench.tsx";
import { HeaderBrand, streamClick, useNow } from "./StreamParts.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { useCompactChrome } from "./use-compact-chrome.ts";
import {
  useHistory,
  useHistoryOverview,
  useHistoryQuery,
} from "./use-history.ts";

/** The recap's row cap: enough for a year of standups, bounded for the clipboard. */
const recapLimit = 1000;

/**
 * History at `/history`: the pull requests the reader's GitHub account opened
 * and merged in followed repositories, by whole local days. The overview
 * charts a range and its repositories; the list pages through pull requests
 * by day, filtered by activity, state now, repository or one day. Facets
 * choose the range and filters; narrow screens choose from menus instead.
 */
export function HistoryPage() {
  const [query, setQuery] = useHistoryQuery();
  const { mobile } = useSidebarState();
  const compact = useCompactChrome();
  const now = useNow();
  const span = historyWindow(query, now);
  const overviewView = query.view === "overview";
  const unit = chartUnit(query.range, compact);
  const edges = historyEdges(span, unit);
  const overview = useHistoryOverview(edges, overviewView);
  const days = listWindow(query, now);
  const list = useHistory(
    days,
    { repo: query.repo, activity: query.activity, state: query.state },
    !overviewView,
  );
  const load = overviewView ? overview.load : list.load;
  const retry = overviewView ? overview.retry : list.retry;
  const loaded = load.status === "ready" ? load.data : null;
  const listData =
    !overviewView && list.load.status === "ready" ? list.load.data : null;
  const totals = overviewView ? null : list.totals;
  // Retention outlives a view switch, so the ranges offered stay put.
  const [retainedSince, setRetainedSince] = useState<number | null>(null);
  if (loaded && loaded.retainedSince !== retainedSince)
    setRetainedSince(loaded.retainedSince);
  const kept =
    retainedSince === null ? null : retentionDays(retainedSince, now);
  const ranges = rangeChoices(kept, query.range);
  const backward = retainedSince !== null && canStepBack(span, retainedSince);
  const filtered = historyFiltered(query, now);
  const label = windowLabel(overviewView ? span : days, now);

  async function recap() {
    const drained = await list.drain(recapLimit);
    if (!drained || !listData) return null;
    return historyRecap(drained.pulls, {
      window: windowLabel(days, now),
      filters: recapFilters(query),
      summary: listSummary(listData, query, days, now),
      capped: drained.capped ? recapLimit : null,
    });
  }

  // Compact screens give the view toggle and list actions their own row.
  const actions = (
    <div className="history-actions">
      {!overviewView && filtered && (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          touch
          onClick={() => setQuery(clearFilters(query))}
        >
          Clear filters
        </Button>
      )}
      {!overviewView && (
        <CopyRecapButton
          compact={compact}
          disabled={list.pulls.length === 0}
          recap={recap}
        />
      )}
      <ViewToggle
        compact={compact}
        view={query.view}
        onChange={(view) => setQuery({ ...query, view })}
      />
    </div>
  );

  return (
    <div className="app-shell">
      <div className="app-main">
        <header className="topbar">
          <HeaderBrand />
          <LinkButton
            variant="shell"
            size="sm"
            touch
            href={benchTrail().path}
            aria-label="Reviews"
            onClick={(event) =>
              streamClick(event, () => navigate(benchTrail().path))
            }
          >
            <ChevronLeftIcon aria-hidden="true" />
            <span className="max-md:hidden">Reviews</span>
          </LinkButton>
          <span
            className="context-identity-separator bench-link-separator"
            aria-hidden="true"
          >
            /
          </span>
          <h1 id="history-title" className="bench-title">
            <HistoryIcon aria-hidden="true" />
            History
          </h1>
          <div className="header-actions">
            <AccountButton variant="shell" />
          </div>
        </header>
        <div className="bench">
          {!mobile && (
            <HistoryFacets
              query={query}
              ranges={ranges}
              data={totals}
              onChange={setQuery}
            />
          )}
          <main className="bench-main" aria-labelledby="history-title">
            <PanelHeader className="bench-toolbar">
              {mobile && (
                <HistoryMenus
                  query={query}
                  ranges={ranges}
                  data={totals}
                  onChange={setQuery}
                />
              )}
              <div className="history-stepper">
                <IconButton
                  variant="ghost"
                  touch
                  label="Earlier"
                  disabled={!backward}
                  onClick={() => setQuery(stepHistory(query, -1, now))}
                >
                  <ChevronLeftIcon aria-hidden="true" />
                </IconButton>
                <span className="history-window" aria-live="polite">
                  {label}
                </span>
                <IconButton
                  variant="ghost"
                  touch
                  label="Later"
                  disabled={span.current}
                  onClick={() => setQuery(stepHistory(query, 1, now))}
                >
                  <ChevronRightIcon aria-hidden="true" />
                </IconButton>
              </div>
              {!compact && actions}
            </PanelHeader>
            {compact && (
              <PanelHeader className="bench-toolbar">{actions}</PanelHeader>
            )}
            <div className="bench-scroll" aria-busy={load.status === "loading"}>
              {load.status === "failed" ? (
                <div className="p-4">
                  <ErrorState message={load.message} onRetry={retry} />
                </div>
              ) : overviewView ? (
                overview.load.status !== "ready" ? (
                  <HistoryOverviewSkeleton />
                ) : !overview.load.data.connected ||
                  !overview.load.data.following ||
                  overview.load.data.opened + overview.load.data.merged ===
                    0 ? (
                  <>
                    {overview.load.data.connected &&
                      overview.load.data.following && (
                        <RetentionNote
                          span={span}
                          retainedSince={overview.load.data.retainedSince}
                        />
                      )}
                    <HistoryEmpty
                      data={overview.load.data}
                      query={query}
                      phrase={windowPhrase(span, now)}
                      onChange={setQuery}
                    />
                  </>
                ) : (
                  <HistoryOverview
                    overview={overview.load.data}
                    query={query}
                    span={span}
                    unit={unit}
                    edges={edges}
                    now={now}
                    onChange={setQuery}
                  />
                )
              ) : listData === null ? (
                <HistorySkeleton />
              ) : (
                <HistoryList
                  data={listData}
                  pulls={list.pulls}
                  query={query}
                  span={days}
                  now={now}
                  hasMore={list.hasMore}
                  more={list.more}
                  onMore={list.loadMore}
                  onChange={setQuery}
                />
              )}
            </div>
            <footer className="main-footer">
              <span className="footer-shortcuts">
                {loaded && loaded.connected && kept !== null
                  ? `Followed repositories · kept ${kept} days`
                  : "Followed repositories"}
              </span>
              <span className="footer-details">
                {overviewView
                  ? overview.load.status === "ready" &&
                    pluralize(
                      overview.load.data.repositories.length,
                      "repository",
                      "repositories",
                    )
                  : listData &&
                    (list.hasMore
                      ? `${formatCount(list.pulls.length)}+ pull requests`
                      : pluralize(list.pulls.length, "pull request"))}
              </span>
            </footer>
          </main>
        </div>
      </div>
    </div>
  );
}

/** Overview or the pull request list; icons alone on compact screens. */
function ViewToggle({
  compact,
  view,
  onChange,
}: {
  compact: boolean;
  view: HistoryView;
  onChange: (view: HistoryView) => void;
}) {
  return (
    <SegmentedControl
      label="History view"
      touch
      value={view}
      onValueChange={onChange}
      options={
        compact
          ? [
              {
                value: "overview",
                label: <ChartColumnIcon aria-hidden="true" />,
                ariaLabel: "Overview",
              },
              {
                value: "list",
                label: <ListIcon aria-hidden="true" />,
                ariaLabel: "Pull requests",
              },
            ]
          : [
              { value: "overview", label: "Overview" },
              { value: "list", label: "Pull requests" },
            ]
      }
    />
  );
}

/**
 * Copies the list as a Markdown recap, reading remaining pages first; a
 * refused clipboard shows the recap for manual copying.
 */
function CopyRecapButton({
  compact,
  disabled,
  recap,
}: {
  compact: boolean;
  disabled: boolean;
  recap: () => Promise<string | null>;
}) {
  const [status, setStatus] = useState<"idle" | "copying" | "copied" | "error">(
    "idle",
  );
  const [manual, setManual] = useState<string | null>(null);
  useEffect(() => {
    if (status !== "copied" && status !== "error") return;
    const timeout = window.setTimeout(() => setStatus("idle"), 2_000);
    return () => window.clearTimeout(timeout);
  }, [status]);

  async function copy() {
    setStatus("copying");
    const text = await recap();
    if (text === null) {
      setStatus("error");
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      setStatus("copied");
    } catch {
      setStatus("idle");
      setManual(text);
    }
  }

  const copied = status === "copied";
  const Icon = copied ? CheckIcon : CopyIcon;
  return (
    <>
      {compact ? (
        <IconButton
          label="Copy recap"
          hint={copied ? "Copied" : "Copy recap"}
          touch
          disabled={disabled || status === "copying"}
          aria-busy={status === "copying"}
          onClick={() => void copy()}
        >
          <Icon aria-hidden="true" />
        </IconButton>
      ) : (
        <Button
          type="button"
          variant="ghost"
          size="sm"
          touch
          disabled={disabled || status === "copying"}
          aria-busy={status === "copying"}
          onClick={() => void copy()}
        >
          <Icon aria-hidden="true" />
          {copied ? "Copied" : "Copy recap"}
        </Button>
      )}
      <span className="visually-hidden" role="status">
        {copied
          ? "Recap copied"
          : status === "error"
            ? "Couldn't load the recap. Try again."
            : ""}
      </span>
      {manual !== null && (
        <CopyDialog
          content={{
            clipboard: "manual",
            label: "Recap",
            text: manual,
            title: "Copy recap",
          }}
          onClose={() => setManual(null)}
        />
      )}
    </>
  );
}

function RetentionNote({
  span,
  retainedSince,
}: {
  span: HistoryWindow;
  retainedSince: number;
}) {
  const note = retentionNote(span, retainedSince);
  return note ? <p className="history-note">{note}</p> : null;
}

function HistoryList({
  data,
  pulls,
  query,
  span,
  now,
  hasMore,
  more,
  onMore,
  onChange,
}: {
  data: HistoryData;
  pulls: HistoryPullRequest[];
  query: HistoryQuery;
  span: HistoryWindow;
  now: number;
  hasMore: boolean;
  more: "idle" | "loading" | "failed";
  onMore: () => void;
  onChange: (query: HistoryQuery) => void;
}) {
  if (!data.connected || !data.following)
    return <HistoryEmpty data={data} query={query} onChange={onChange} />;
  if (!pulls.length)
    return (
      <>
        <RetentionNote span={span} retainedSince={data.retainedSince} />
        <HistoryEmpty
          data={data}
          query={query}
          filtered={historyFiltered(query, now)}
          phrase={windowPhrase(span, now)}
          onChange={onChange}
        />
      </>
    );
  return (
    <>
      <p className="history-summary">{listSummary(data, query, span, now)}</p>
      <RetentionNote span={span} retainedSince={data.retainedSince} />
      {historyDays(pulls, now).map((day) => (
        <section key={day.id} aria-labelledby={`history-day-${day.id}`}>
          <div className="bench-section">
            <h2 id={`history-day-${day.id}`}>{day.title}</h2>
            <CountBadge count={day.pulls.length} />
          </div>
          <ul className="bench-list" aria-label={day.title}>
            {day.pulls.map((pull) => (
              <li key={`${pull.repositoryId}#${pull.number}`}>
                <HistoryRow pull={pull} />
              </li>
            ))}
          </ul>
        </section>
      ))}
      {hasMore && (
        <HistoryMore loaded={pulls.length} more={more} onMore={onMore} />
      )}
    </>
  );
}

/** The list's end: reading on when it scrolls into view, or on request. */
function HistoryMore({
  loaded,
  more,
  onMore,
}: {
  loaded: number;
  more: "idle" | "loading" | "failed";
  onMore: () => void;
}) {
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const node = sentinel.current;
    if (!node || more !== "idle" || typeof IntersectionObserver === "undefined")
      return;
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) onMore();
    });
    observer.observe(node);
    return () => observer.disconnect();
  }, [more, onMore, loaded]);
  return (
    <div ref={sentinel} className="history-more">
      <Button
        type="button"
        variant="ghost"
        size="sm"
        touch
        disabled={more === "loading"}
        aria-busy={more === "loading"}
        onClick={onMore}
      >
        {more === "loading" ? "Loading…" : "Load more"}
      </Button>
      {more === "failed" && (
        <span className="text-xs text-error" role="status">
          Couldn't load more pull requests. Try again.
        </span>
      )}
    </div>
  );
}

/** One pull request, opening on GitHub in a new tab. */
function HistoryRow({ pull }: { pull: HistoryPullRequest }) {
  const toMerge = openToMerge(pull);
  return (
    <ListRow
      variant="table"
      depth={1}
      className="history-row"
      render={<a href={pull.url} target="_blank" rel="noopener noreferrer" />}
      title={`${pull.fullName}#${pull.number} · ${pull.title} · Open on GitHub`}
    >
      <PullIcon state={pullState(pull)} />
      <span className="min-w-0 truncate">
        <span className="text-muted tabular-nums">#{pull.number}</span>{" "}
        {pull.title}
      </span>
      <span className="min-w-0 truncate font-mono text-xs text-secondary">
        {pull.fullName}
      </span>
      <span className="text-xs text-secondary">{eventLabel(pull)}</span>
      <span
        className="history-duration"
        title={toMerge === null ? undefined : "Time from open to merge"}
      >
        {toMerge === null ? "" : formatDuration(toMerge)}
      </span>
      <span className="text-right font-mono text-xs text-muted tabular-nums">
        {eventClock(pull)}
      </span>
    </ListRow>
  );
}

function HistorySkeleton() {
  return (
    <div role="status" className="flex flex-col gap-3 p-4">
      {Array.from({ length: 6 }, (_, index) => (
        <Skeleton key={index} shape="control" className="w-full" />
      ))}
      <p className="visually-hidden">Loading history…</p>
    </div>
  );
}

const activityFacets = [
  { value: "merged", label: "Merged", Icon: GitMergeIcon },
  { value: "opened", label: "Opened", Icon: GitPullRequestArrowIcon },
] as const;

const stateFacets: HistoryPullState[] = ["open", "draft", "closed", "merged"];

function HistoryFacets({
  query,
  ranges,
  data,
  onChange,
}: {
  query: HistoryQuery;
  ranges: HistoryRangeOption[];
  /** The list's first page; null in the overview, which filters nothing. */
  data: HistoryData | null;
  onChange: (query: HistoryQuery) => void;
}) {
  const list = query.view === "list";
  return (
    <nav className="bench-facets" aria-label="History filters">
      <PanelHeader title="Range" />
      <div className="bench-facets-scroll">
        <ul className="bench-list" aria-label="Range">
          {ranges.map((range) => {
            const next = { ...query, range: range.value, end: "", day: "" };
            return (
              <li key={range.value}>
                <ListRow
                  depth={1}
                  render={<a href={historyPath(next)} />}
                  aria-current={
                    query.range === range.value ? "true" : undefined
                  }
                  onClick={(event) => streamClick(event, () => onChange(next))}
                >
                  <CalendarIcon aria-hidden="true" />
                  <span className="min-w-0 flex-1 truncate">{range.label}</span>
                </ListRow>
              </li>
            );
          })}
        </ul>
        {list && (
          <>
            <h2 className="bench-facet-title">Activity</h2>
            <ul className="bench-list" aria-label="Activity">
              {activityFacets.map((facet) => {
                const current = query.activity === facet.value;
                const next: HistoryQuery = {
                  ...query,
                  activity: current ? "" : facet.value,
                };
                return (
                  <li key={facet.value}>
                    <FacetLink
                      href={historyPath(next)}
                      current={current}
                      Icon={facet.Icon}
                      label={facet.label}
                      count={data?.[facet.value] ?? 0}
                      onSelect={() => onChange(next)}
                    />
                  </li>
                );
              })}
            </ul>
            <h2 className="bench-facet-title">State now</h2>
            <ul className="bench-list" aria-label="State now">
              {stateFacets.map((state) => {
                const current = query.state === state;
                const next: HistoryQuery = {
                  ...query,
                  state: current ? "" : state,
                };
                return (
                  <li key={state}>
                    <FacetLink
                      href={historyPath(next)}
                      current={current}
                      Icon={pullStateIcons[state]}
                      label={stateLabels[state]}
                      count={data?.states[state] ?? 0}
                      onSelect={() => onChange(next)}
                    />
                  </li>
                );
              })}
            </ul>
          </>
        )}
        {list && data && data.repositories.length > 0 && (
          <>
            <h2 className="bench-facet-title history-facet-title">
              Repositories
              <span aria-hidden="true">merged · opened</span>
            </h2>
            <ul className="bench-list" aria-label="Repositories">
              {data.repositories.map((repository) => {
                const current = query.repo === repository.fullName;
                const next = {
                  ...query,
                  repo: current ? "" : repository.fullName,
                };
                return (
                  <li key={repository.fullName}>
                    <ListRow
                      depth={1}
                      render={<a href={historyPath(next)} />}
                      aria-current={current ? "true" : undefined}
                      onClick={(event) =>
                        streamClick(event, () => onChange(next))
                      }
                    >
                      <FolderGit2Icon aria-hidden="true" />
                      <span className="min-w-0 flex-1 truncate font-mono text-xs">
                        {repository.fullName}
                      </span>
                      <span
                        className="history-repo-counts"
                        aria-label={`${repository.merged} merged, ${repository.opened} opened`}
                      >
                        {repository.merged} · {repository.opened}
                      </span>
                    </ListRow>
                  </li>
                );
              })}
            </ul>
          </>
        )}
      </div>
    </nav>
  );
}

/** Narrow screens choose the range and filters from menus. */
function HistoryMenus({
  query,
  ranges,
  data,
  onChange,
}: {
  query: HistoryQuery;
  ranges: HistoryRangeOption[];
  data: HistoryData | null;
  onChange: (query: HistoryQuery) => void;
}) {
  const range = ranges.find((item) => item.value === query.range);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button type="button" variant="ghost" size="sm" touch>
              <CalendarIcon aria-hidden="true" />
              {range?.label}
            </Button>
          }
        />
        <DropdownMenuContent align="start" aria-label="Range">
          <DropdownMenuRadioGroup
            aria-label="Range"
            value={query.range}
            onValueChange={(next) => {
              const chosen = ranges.find((item) => item.value === next);
              if (chosen)
                onChange({ ...query, range: chosen.value, end: "", day: "" });
            }}
          >
            {ranges.map((item) => (
              <DropdownMenuRadioItem
                key={item.value}
                value={item.value}
                closeOnClick
              >
                {item.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      {query.view === "list" && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button type="button" variant="ghost" size="sm" touch>
                <ListFilterIcon aria-hidden="true" />
                Filter
              </Button>
            }
          />
          <DropdownMenuContent align="start" aria-label="Filter pull requests">
            <DropdownMenuGroup>
              <DropdownMenuLabel>Activity</DropdownMenuLabel>
              <DropdownMenuRadioGroup
                aria-label="Activity"
                value={query.activity || "all"}
                onValueChange={(next) => {
                  const activity: HistoryActivity =
                    next === "opened" || next === "merged" ? next : "";
                  onChange({ ...query, activity });
                }}
              >
                <DropdownMenuRadioItem value="all">All</DropdownMenuRadioItem>
                {activityFacets.map((facet) => (
                  <DropdownMenuRadioItem key={facet.value} value={facet.value}>
                    {facet.label}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>State now</DropdownMenuLabel>
              <DropdownMenuRadioGroup
                aria-label="State now"
                value={query.state || "all"}
                onValueChange={(next) => {
                  const state = stateFacets.find((item) => item === next);
                  onChange({ ...query, state: state ?? "" });
                }}
              >
                <DropdownMenuRadioItem value="all">All</DropdownMenuRadioItem>
                {stateFacets.map((state) => (
                  <DropdownMenuRadioItem key={state} value={state}>
                    {stateLabels[state]}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuGroup>
            {data && data.repositories.length > 0 && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuLabel>Repositories</DropdownMenuLabel>
                  <DropdownMenuRadioGroup
                    aria-label="Repository"
                    value={query.repo || "all"}
                    onValueChange={(next) =>
                      onChange({ ...query, repo: next === "all" ? "" : next })
                    }
                  >
                    <DropdownMenuRadioItem value="all">
                      All
                    </DropdownMenuRadioItem>
                    {data.repositories.map((repository) => (
                      <DropdownMenuRadioItem
                        key={repository.fullName}
                        value={repository.fullName}
                      >
                        {repository.fullName}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuGroup>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </>
  );
}
