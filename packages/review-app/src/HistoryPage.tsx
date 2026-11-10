import { api, errorDetail } from "@diffx/api";
import {
  CalendarIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  FolderGit2Icon,
  GitMergeIcon,
  GitPullRequestArrowIcon,
  GitPullRequestClosedIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
  HistoryIcon,
  ListFilterIcon,
} from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { ErrorState } from "@diffx/ui/components/error-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { LinkButton } from "@diffx/ui/components/link-button";
import { ListRow } from "@diffx/ui/components/list-row";
import { PanelHeader } from "@diffx/ui/components/panel-header";
import { Button } from "@diffx/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import { pluralize } from "@diffx/ui/lib/format";
import { AccountButton } from "./account.tsx";
import { benchTrail } from "./bench-trail.ts";
import {
  activityCounts,
  canStepBack,
  eventClock,
  eventLabel,
  historyDays,
  historyPath,
  historyQuery,
  historyRanges,
  historyRoute,
  historySummary,
  historyWindow,
  matchesHistory,
  retentionDays,
  retentionNote,
  stepHistory,
  windowLabel,
  windowPhrase,
  type HistoryActivity,
  type HistoryData,
  type HistoryPullRequest,
  type HistoryQuery,
  type HistoryWindow,
} from "./history.ts";
import { navigate } from "./identity-context.tsx";
import { FacetLink } from "./ReviewBench.tsx";
import { settingsPath } from "./settings-layout.tsx";
import { HeaderBrand, streamClick, useNow } from "./StreamParts.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

function useHistoryQuery() {
  const [query, setQueryState] = useState(() =>
    historyQuery(window.location.search),
  );
  useEffect(() => {
    const changed = () => {
      if (window.location.pathname === historyRoute)
        setQueryState(historyQuery(window.location.search));
    };
    window.addEventListener("popstate", changed);
    return () => window.removeEventListener("popstate", changed);
  }, []);
  const setQuery = useCallback((next: HistoryQuery) => {
    window.history.pushState(null, "", historyPath(next));
    setQueryState(next);
  }, []);
  return [query, setQuery] as const;
}

type Load =
  | { status: "loading" }
  | { status: "failed"; message: string }
  | { status: "ready"; data: HistoryData };

/** Reads the window's history, again on Retry and when the window changes. */
function useHistory(span: HistoryWindow) {
  const { since, until } = span;
  const [load, setLoad] = useState<Load>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    const failure = "Unable to load history. Try again.";
    setLoad({ status: "loading" });
    void api
      .GET("/api/v2/github/history", {
        params: { query: { since, until } },
        signal: controller.signal,
      })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data) throw new Error(errorDetail(error, failure));
        setLoad({ status: "ready", data });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted)
          setLoad({ status: "failed", message: errorDetail(cause, failure) });
      });
    return () => controller.abort();
  }, [since, until, attempt]);
  return { load, retry: () => setAttempt((value) => value + 1) };
}

/**
 * History at `/history`: the pull requests the reader's GitHub account opened
 * and merged in followed repositories, by whole local days. Facets choose the
 * range and filter by activity or repository; the table lists pull requests
 * by day, each linking to GitHub. Narrow screens choose from menus instead.
 */
export function HistoryPage() {
  const [query, setQuery] = useHistoryQuery();
  const { mobile } = useSidebarState();
  const now = useNow();
  const span = historyWindow(query, now);
  const { load, retry } = useHistory(span);
  const data = load.status === "ready" ? load.data : null;
  const retainedSince = data?.retainedSince ?? 0;
  const backward = data !== null && canStepBack(span, retainedSince);
  const filtered = Boolean(query.activity || query.repo);
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
            <HistoryFacets query={query} data={data} onChange={setQuery} />
          )}
          <main className="bench-main" aria-labelledby="history-title">
            <PanelHeader className="bench-toolbar">
              {mobile && (
                <HistoryMenus query={query} data={data} onChange={setQuery} />
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
                  {windowLabel(span, now)}
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
              {filtered && (
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="ml-auto"
                  onClick={() => setQuery({ ...query, activity: "", repo: "" })}
                >
                  Clear filters
                </Button>
              )}
            </PanelHeader>
            <div className="bench-scroll" aria-busy={load.status === "loading"}>
              {load.status === "failed" ? (
                <div className="p-4">
                  <ErrorState message={load.message} onRetry={retry} />
                </div>
              ) : data === null ? (
                <HistorySkeleton />
              ) : (
                <HistoryList
                  data={data}
                  query={query}
                  span={span}
                  now={now}
                  onChange={setQuery}
                />
              )}
            </div>
            <footer className="main-footer">
              <span className="footer-shortcuts">
                {data && data.connected
                  ? `Followed repositories · kept ${retentionDays(data.retainedSince, now)} days`
                  : "Followed repositories"}
              </span>
              <span className="footer-details">
                {data &&
                  pluralize(
                    data.pullRequests.filter((pull) =>
                      matchesHistory(pull, query),
                    ).length,
                    "pull request",
                  )}
              </span>
            </footer>
          </main>
        </div>
      </div>
    </div>
  );
}

function HistoryList({
  data,
  query,
  span,
  now,
  onChange,
}: {
  data: HistoryData;
  query: HistoryQuery;
  span: HistoryWindow;
  now: number;
  onChange: (query: HistoryQuery) => void;
}) {
  const note = retentionNote(span, data.retainedSince);
  if (!data.connected || !data.following)
    return <HistoryEmpty data={data} query={query} onChange={onChange} />;
  const pulls = data.pullRequests.filter((pull) => matchesHistory(pull, query));
  if (!pulls.length)
    return (
      <>
        {note && <p className="history-note">{note}</p>}
        <HistoryEmpty
          data={data}
          query={query}
          phrase={windowPhrase(span, now)}
          onChange={onChange}
        />
      </>
    );
  const counts = activityCounts(data, query.repo);
  const repositories = new Set(pulls.map((pull) => pull.fullName)).size;
  return (
    <>
      <p className="history-summary">
        {historySummary(
          query.activity === "merged" ? 0 : counts.opened,
          query.activity === "opened" ? 0 : counts.merged,
          repositories,
          windowPhrase(span, now),
        )}
      </p>
      {note && <p className="history-note">{note}</p>}
      {data.truncated && (
        <p className="history-note">
          Showing the latest 1,000 pull requests; counts cover only those.
        </p>
      )}
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
    </>
  );
}

function PullIcon({ pull }: { pull: HistoryPullRequest }) {
  const [Icon, label] =
    pull.state === "merged"
      ? [GitMergeIcon, "Merged pull request"]
      : pull.state === "closed"
        ? [GitPullRequestClosedIcon, "Closed pull request"]
        : pull.draft
          ? [GitPullRequestDraftIcon, "Draft pull request"]
          : [GitPullRequestIcon, "Open pull request"];
  return <Icon className="text-muted" role="img" aria-label={label} />;
}

/** One pull request, opening on GitHub in a new tab. */
function HistoryRow({ pull }: { pull: HistoryPullRequest }) {
  const event = eventLabel(pull);
  return (
    <ListRow
      variant="table"
      depth={1}
      className="history-row"
      render={<a href={pull.url} target="_blank" rel="noopener noreferrer" />}
      title={`${pull.fullName}#${pull.number} · ${pull.title} · Open on GitHub`}
    >
      <PullIcon pull={pull} />
      <span className="min-w-0 truncate">
        <span className="text-muted tabular-nums">#{pull.number}</span>{" "}
        {pull.title}
      </span>
      <span className="min-w-0 truncate font-mono text-xs text-secondary">
        {pull.fullName}
      </span>
      <span className="text-xs text-secondary">{event}</span>
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

function SettingsAction({ label }: { label: string }) {
  const href = settingsPath("github");
  return (
    <LinkButton
      variant="outline"
      href={href}
      onClick={(event) => streamClick(event, () => navigate(href))}
    >
      {label}
    </LinkButton>
  );
}

function HistoryEmpty({
  data,
  query,
  phrase,
  onChange,
}: {
  data: HistoryData;
  query: HistoryQuery;
  phrase?: string;
  onChange: (query: HistoryQuery) => void;
}) {
  let title: string;
  let description: string;
  let action: ReactNode = null;
  if (!data.connected) {
    title = "Connect GitHub to see your history";
    description =
      "History lists the pull requests you open and merge in repositories you follow.";
    action = <SettingsAction label="Connect GitHub" />;
  } else if (!data.following) {
    title = "Follow repositories to see your history";
    description = `History lists the pull requests @${data.login ?? "you"} opens and merges in repositories you follow.`;
    action = <SettingsAction label="Follow repositories" />;
  } else if (query.activity || query.repo) {
    title = "No matching pull requests";
    description = "No pull request in this range matches the filters.";
    action = (
      <Button
        type="button"
        variant="outline"
        onClick={() => onChange({ ...query, activity: "", repo: "" })}
      >
        Clear filters
      </Button>
    );
  } else {
    title = `No pull requests ${phrase ?? ""}`.trim();
    description =
      "Pull requests you open or merge in followed repositories appear here.";
    if (query.range !== "30d")
      action = (
        <Button
          type="button"
          variant="outline"
          onClick={() => onChange({ ...query, range: "30d", end: "" })}
        >
          Show 30 days
        </Button>
      );
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

const activityFacets = [
  { value: "merged", label: "Merged", Icon: GitMergeIcon },
  { value: "opened", label: "Opened", Icon: GitPullRequestArrowIcon },
] as const;

function HistoryFacets({
  query,
  data,
  onChange,
}: {
  query: HistoryQuery;
  data: HistoryData | null;
  onChange: (query: HistoryQuery) => void;
}) {
  const counts = data ? activityCounts(data, query.repo) : null;
  return (
    <nav className="bench-facets" aria-label="History filters">
      <PanelHeader title="Range" />
      <div className="bench-facets-scroll">
        <ul className="bench-list" aria-label="Range">
          {historyRanges.map((range) => {
            const next = { ...query, range: range.value, end: "" };
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
                  count={counts?.[facet.value] ?? 0}
                  onSelect={() => onChange(next)}
                />
              </li>
            );
          })}
        </ul>
        {data && data.repositories.length > 0 && (
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
  data,
  onChange,
}: {
  query: HistoryQuery;
  data: HistoryData | null;
  onChange: (query: HistoryQuery) => void;
}) {
  const range = historyRanges.find((item) => item.value === query.range);
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
              const chosen = historyRanges.find((item) => item.value === next);
              if (chosen) onChange({ ...query, range: chosen.value, end: "" });
            }}
          >
            {historyRanges.map((item) => (
              <DropdownMenuRadioItem key={item.value} value={item.value}>
                {item.label}
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
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
          {data && data.repositories.length > 0 && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>Repositories</DropdownMenuLabel>
              <DropdownMenuRadioGroup
                aria-label="Repository"
                value={query.repo || "all"}
                onValueChange={(next) =>
                  onChange({ ...query, repo: next === "all" ? "" : next })
                }
              >
                <DropdownMenuRadioItem value="all">All</DropdownMenuRadioItem>
                {data.repositories.map((repository) => (
                  <DropdownMenuRadioItem
                    key={repository.fullName}
                    value={repository.fullName}
                  >
                    {repository.fullName}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </>
  );
}
