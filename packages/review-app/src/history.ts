import type {
  ApiGitHubHistory as HistoryData,
  ApiGitHubHistoryOverview as HistoryOverviewData,
  ApiGitHubHistoryPullRequest as HistoryPullRequest,
} from "@diffx/api";

export type { HistoryData, HistoryOverviewData, HistoryPullRequest };

export const historyRanges = [
  { value: "today", label: "Today", days: 1 },
  { value: "7d", label: "7 days", days: 7 },
  { value: "30d", label: "30 days", days: 30 },
  { value: "90d", label: "90 days", days: 90 },
  // 52 whole weeks, so the year charts as weekly blocks.
  { value: "1y", label: "1 year", days: 364 },
] as const;

export type HistoryRange = (typeof historyRanges)[number]["value"];
export type HistoryRangeOption = (typeof historyRanges)[number];
export type HistoryActivity = "" | "opened" | "merged";
export type HistoryView = "overview" | "list";
/** A pull request's state now: open excludes drafts. */
export type HistoryPullState = "open" | "draft" | "closed" | "merged";
export type HistoryStateFilter = "" | HistoryPullState;

/**
 * History's URL state: the view, a range of whole local days ending on `end`
 * (YYYY-MM-DD; empty for today), the list's filters (activity, current state,
 * repository) and `day`, one local day of the range the list narrows to.
 */
export type HistoryQuery = {
  view: HistoryView;
  range: HistoryRange;
  end: string;
  activity: HistoryActivity;
  state: HistoryStateFilter;
  repo: string;
  day: string;
};

export const defaultHistoryQuery: HistoryQuery = {
  view: "overview",
  range: "7d",
  end: "",
  activity: "",
  state: "",
  repo: "",
  day: "",
};

export const historyRoute = "/history";

function rangeOf(value: string | null) {
  return historyRanges.find((range) => range.value === value);
}

function activityOf(value: string | null): HistoryActivity {
  return value === "opened" || value === "merged" ? value : "";
}

function stateOf(value: string | null): HistoryStateFilter {
  return value === "open" ||
    value === "draft" ||
    value === "closed" ||
    value === "merged"
    ? value
    : "";
}

/**
 * Ranges retained history can fill: 90 days and a year only when kept that
 * long. Before the first response (null) only the short ranges show.
 */
export function availableRanges(
  retentionDays: number | null,
): HistoryRangeOption[] {
  return historyRanges.filter(
    (range) =>
      range.days <= 30 ||
      (retentionDays !== null && retentionDays >= range.days),
  );
}

/** The ranges to offer: available ones, plus the query's own when typed. */
export function rangeChoices(
  retentionDays: number | null,
  current: HistoryRange,
): HistoryRangeOption[] {
  const available = availableRanges(retentionDays);
  return historyRanges.filter(
    (range) => range.value === current || available.includes(range),
  );
}

const dayPattern = /^(\d{4})-(\d{2})-(\d{2})$/;

/** The local midnight a YYYY-MM-DD names; null when malformed. */
export function parseDay(value: string) {
  const match = dayPattern.exec(value);
  if (!match) return null;
  const [year, month, date] = [match[1], match[2], match[3]].map(Number);
  if (year === undefined || month === undefined || date === undefined)
    return null;
  const day = new Date(year, month - 1, date);
  return day.getFullYear() === year &&
    day.getMonth() === month - 1 &&
    day.getDate() === date
    ? day
    : null;
}

export function formatDay(day: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}`;
}

function startOfDay(at: number) {
  const day = new Date(at);
  return new Date(day.getFullYear(), day.getMonth(), day.getDate());
}

/** `day` moved by whole calendar days, keeping local midnight across DST. */
function addDays(day: Date, days: number) {
  return new Date(day.getFullYear(), day.getMonth(), day.getDate() + days);
}

export function historyQuery(search: string): HistoryQuery {
  const params = new URLSearchParams(search);
  const end = params.get("end") ?? "";
  const day = params.get("day") ?? "";
  return {
    view: params.get("view") === "list" ? "list" : "overview",
    range: rangeOf(params.get("range"))?.value ?? defaultHistoryQuery.range,
    end: parseDay(end) ? end : "",
    activity: activityOf(params.get("activity")),
    state: stateOf(params.get("state")),
    repo: params.get("repo") ?? "",
    day: parseDay(day) ? day : "",
  };
}

export function historyPath(query: HistoryQuery) {
  const params = new URLSearchParams();
  if (query.view !== defaultHistoryQuery.view) params.set("view", query.view);
  if (query.range !== defaultHistoryQuery.range)
    params.set("range", query.range);
  if (query.end) params.set("end", query.end);
  if (query.activity) params.set("activity", query.activity);
  if (query.state) params.set("state", query.state);
  if (query.repo) params.set("repo", query.repo);
  if (query.day) params.set("day", query.day);
  const search = params.toString();
  return search ? `${historyRoute}?${search}` : historyRoute;
}

/** The query without the list's filters. */
export function clearFilters(query: HistoryQuery): HistoryQuery {
  return { ...query, activity: "", state: "", repo: "", day: "" };
}

export type HistoryWindow = {
  /** Local midnight starting the first day, Unix ms, inclusive. */
  since: number;
  /** Local midnight after the last day, Unix ms, exclusive. */
  until: number;
  /** The last day, at local midnight. */
  last: Date;
  days: number;
  /** Whether the window ends today. */
  current: boolean;
};

/** The whole local days the query covers; an end after today is today. */
export function historyWindow(query: HistoryQuery, now: number): HistoryWindow {
  const days = rangeOf(query.range)?.days ?? 7;
  const today = startOfDay(now);
  const requested = parseDay(query.end);
  const last = requested && requested < today ? requested : today;
  return {
    since: addDays(last, 1 - days).getTime(),
    until: addDays(last, 1).getTime(),
    last,
    days,
    current: last.getTime() === today.getTime(),
  };
}

/**
 * The query one window earlier (-1) or later (1). Later never passes today;
 * reaching today drops `end`. Stepping leaves the chosen day behind.
 */
export function stepHistory(
  query: HistoryQuery,
  direction: -1 | 1,
  now: number,
): HistoryQuery {
  const window = historyWindow(query, now);
  const last = addDays(window.last, direction * window.days);
  const today = startOfDay(now);
  return { ...query, end: last < today ? formatDay(last) : "", day: "" };
}

/** Whether the previous window still overlaps retained history. */
export function canStepBack(window: HistoryWindow, retainedSince: number) {
  return window.since > retainedSince;
}

const dayMonth = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
});
const dayMonthYear = new Intl.DateTimeFormat("en-GB", {
  day: "numeric",
  month: "short",
  year: "numeric",
});
const weekdayDayMonth = new Intl.DateTimeFormat("en-GB", {
  weekday: "short",
  day: "numeric",
  month: "short",
});
/** "06:00" in the local zone. */
function timeOfDay(at: number | Date) {
  const time = new Date(at);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${pad(time.getHours())}:${pad(time.getMinutes())}`;
}

/** "Today", "Yesterday", or "Sat 8 Nov"; another year adds it. */
export function dayLabel(day: Date, now: number) {
  const today = startOfDay(now);
  if (day.getTime() === today.getTime()) return "Today";
  if (day.getTime() === addDays(today, -1).getTime()) return "Yesterday";
  const label = weekdayDayMonth.format(day).replace(",", "");
  return day.getFullYear() === today.getFullYear()
    ? label
    : `${label} ${day.getFullYear()}`;
}

/** The window's days: "4 – 10 Nov", "28 Oct – 3 Nov", or one day's label. */
export function windowLabel(window: HistoryWindow, now: number) {
  if (window.days === 1) return dayLabel(window.last, now);
  const first = new Date(window.since);
  const thisYear = new Date(now).getFullYear();
  const end =
    window.last.getFullYear() === thisYear
      ? dayMonth.format(window.last)
      : dayMonthYear.format(window.last);
  const start =
    first.getFullYear() !== window.last.getFullYear()
      ? dayMonthYear.format(first)
      : first.getMonth() === window.last.getMonth()
        ? String(first.getDate())
        : dayMonth.format(first);
  return `${start} – ${end}`;
}

/** How the summary names the window: "today", "in the last 7 days", "from 4 to 10 Nov". */
export function windowPhrase(window: HistoryWindow, now: number) {
  if (window.current)
    return window.days === 1 ? "today" : `in the last ${window.days} days`;
  if (window.days === 1) {
    const label = dayLabel(window.last, now);
    return label === "Yesterday" ? "yesterday" : `on ${label}`;
  }
  return `from ${windowLabel(window, now).replace(" – ", " to ")}`;
}

function countPhrase(count: number) {
  return count === 1 ? "1 pull request" : `${count} pull requests`;
}

/** One sentence summing up the range's counts. */
export function historySummary(
  opened: number,
  merged: number,
  repositories: number,
  phrase: string,
) {
  const parts =
    merged && opened
      ? `merged ${merged} and opened ${countPhrase(opened)}`
      : merged
        ? `merged ${countPhrase(merged)}`
        : `opened ${countPhrase(opened)}`;
  const across = repositories > 1 ? ` across ${repositories} repositories` : "";
  return `You ${parts}${across} ${phrase}.`;
}

/**
 * The list's summary from the window's totals: within the repository filter,
 * counting only the filtered activity.
 */
export function listSummary(
  data: HistoryData,
  query: HistoryQuery,
  window: HistoryWindow,
  now: number,
) {
  const counts = activityCounts(data, query.repo);
  const opened = query.activity === "merged" ? 0 : counts.opened;
  const merged = query.activity === "opened" ? 0 : counts.merged;
  const repositories = query.repo
    ? 1
    : data.repositories.filter(
        (repository) =>
          (opened > 0 && repository.opened > 0) ||
          (merged > 0 && repository.merged > 0),
      ).length;
  return historySummary(
    opened,
    merged,
    repositories,
    windowPhrase(window, now),
  );
}

/** The latest of a pull request's in-range events, Unix ms. */
export function eventTime(pull: HistoryPullRequest) {
  return pull.merged && pull.mergedAt !== null ? pull.mergedAt : pull.createdAt;
}

/** Which of its events fall in the range. */
export function eventLabel(pull: HistoryPullRequest) {
  if (pull.opened && pull.merged) return "Opened · Merged";
  return pull.merged ? "Merged" : "Opened";
}

export function eventClock(pull: HistoryPullRequest) {
  return timeOfDay(eventTime(pull));
}

export type HistoryDay = {
  /** The day's local midnight, Unix ms. */
  id: number;
  title: string;
  pulls: HistoryPullRequest[];
};

/** Pull requests grouped by the local day of their latest in-range event, newest first. */
export function historyDays(
  pulls: HistoryPullRequest[],
  now: number,
): HistoryDay[] {
  const days = new Map<number, HistoryPullRequest[]>();
  const sorted = pulls.toSorted((a, b) => eventTime(b) - eventTime(a));
  for (const pull of sorted) {
    const id = startOfDay(eventTime(pull)).getTime();
    days.set(id, [...(days.get(id) ?? []), pull]);
  }
  return [...days].map(([id, list]) => ({
    id,
    title: dayLabel(new Date(id), now),
    pulls: list,
  }));
}

/** Activity counts over the fetched window, within the repository filter. */
export function activityCounts(data: HistoryData, repo: string) {
  const repository = repo
    ? data.repositories.find((item) => item.fullName === repo)
    : undefined;
  if (repo)
    return { opened: repository?.opened ?? 0, merged: repository?.merged ?? 0 };
  return { opened: data.opened, merged: data.merged };
}

/** "History before 11 Oct isn't kept." when the window starts earlier. */
export function retentionNote(window: HistoryWindow, retainedSince: number) {
  if (window.since >= retainedSince) return "";
  const kept = startOfDay(retainedSince);
  const first = kept.getTime() < retainedSince ? addDays(kept, 1) : kept;
  return `History before ${dayMonth.format(first)} isn't kept.`;
}

export function retentionDays(retainedSince: number, now: number) {
  return Math.round((now - retainedSince) / 86_400_000);
}

/**
 * The day the list narrows to: `day` when it falls in the query's window,
 * otherwise null (a day outside the window is ignored).
 */
export function activeDay(query: HistoryQuery, now: number) {
  const day = parseDay(query.day);
  if (!day) return null;
  const window = historyWindow(query, now);
  const at = day.getTime();
  return at >= window.since && at < window.until ? day : null;
}

/** Whether the list narrows by activity, state, repository or a day in range. */
export function historyFiltered(query: HistoryQuery, now: number) {
  return Boolean(
    query.activity || query.state || query.repo || activeDay(query, now),
  );
}

/** The days the list reads: the active day, otherwise the query's window. */
export function listWindow(query: HistoryQuery, now: number): HistoryWindow {
  const day = activeDay(query, now);
  if (!day) return historyWindow(query, now);
  return {
    since: day.getTime(),
    until: addDays(day, 1).getTime(),
    last: day,
    days: 1,
    current: day.getTime() === startOfDay(now).getTime(),
  };
}

/** A pull request's state now. */
export function pullState(pull: {
  state: HistoryPullRequest["state"];
  draft: boolean;
}): HistoryPullState {
  if (pull.state === "merged" || pull.state === "closed") return pull.state;
  return pull.draft ? "draft" : "open";
}

const minute = 60_000;
const hour = 60 * minute;

/** A span as "42m", "5h 12m" or "2d 3h"; under a minute is "<1m". */
export function formatDuration(ms: number) {
  if (ms < minute) return "<1m";
  if (ms < hour) return `${Math.floor(ms / minute)}m`;
  if (ms < 24 * hour) {
    const minutes = Math.floor((ms % hour) / minute);
    const hours = `${Math.floor(ms / hour)}h`;
    return minutes ? `${hours} ${minutes}m` : hours;
  }
  const hours = Math.floor((ms % (24 * hour)) / hour);
  const days = `${Math.floor(ms / (24 * hour))}d`;
  return hours ? `${days} ${hours}h` : days;
}

/** Milliseconds from opening to merging; null until merged. */
export function openToMerge(pull: HistoryPullRequest) {
  return pull.mergedAt === null ? null : pull.mergedAt - pull.createdAt;
}

/** How the chart bins a range: hours of a day, local days, or weeks. */
export type ChartUnit = "hour" | "day" | "week";

/** Today charts hours; a year weeks; 90 days weeks on compact screens. */
export function chartUnit(range: HistoryRange, compact: boolean): ChartUnit {
  if (range === "today") return "hour";
  if (range === "1y" || (range === "90d" && compact)) return "week";
  return "day";
}

/**
 * The overview's bin edges, Unix ms, strictly increasing: absolute hours from
 * the window's local midnight (23 to 25 across daylight saving), local
 * midnights, or 7-day blocks counted back from the window's end (the first
 * block may be partial).
 */
export function historyEdges(window: HistoryWindow, unit: ChartUnit) {
  const edges: number[] = [];
  if (unit === "hour") {
    for (let at = window.since; at < window.until; at += hour) edges.push(at);
    edges.push(window.until);
    return edges;
  }
  const first = new Date(window.since);
  if (unit === "day") {
    for (let index = 0; index <= window.days; index++)
      edges.push(addDays(first, index).getTime());
    return edges;
  }
  const end = addDays(window.last, 1);
  for (let back = 0; ; back += 7) {
    const at = addDays(end, -back).getTime();
    if (at <= window.since) break;
    edges.push(at);
  }
  edges.push(window.since);
  return edges.reverse();
}

export type HistoryBinSpan = { since: number; until: number };

/** Bin i spans [edges[i], edges[i + 1]). */
export function binSpans(edges: readonly number[]): HistoryBinSpan[] {
  return edges
    .slice(1)
    .map((until, index) => ({ since: edges[index] ?? until, until }));
}

function blockWindow(span: HistoryBinSpan, now: number): HistoryWindow {
  const first = startOfDay(span.since);
  const last = addDays(startOfDay(span.until), -1);
  return {
    since: span.since,
    until: span.until,
    last,
    days: Math.round((last.getTime() - first.getTime()) / 86_400_000) + 1,
    current: last.getTime() === startOfDay(now).getTime(),
  };
}

/** A bin's full name: "06:00 – 07:00", "Sat 7 Nov" or "2 – 8 Nov". */
export function binLabel(span: HistoryBinSpan, unit: ChartUnit, now: number) {
  if (unit === "hour")
    return `${timeOfDay(span.since)} – ${timeOfDay(span.until)}`;
  if (unit === "day") return dayLabel(startOfDay(span.since), now);
  return windowLabel(blockWindow(span, now), now);
}

const weekdayDate = new Intl.DateTimeFormat("en-GB", {
  weekday: "short",
  day: "numeric",
});
const monthName = new Intl.DateTimeFormat("en-GB", { month: "short" });

/**
 * A bin's axis tick, or undefined: every 6 hours ("06:00"), every day of a
 * week ("Sat 8"), Mondays of longer ranges ("9 Nov"), and month starts within
 * weekly blocks ("Nov").
 */
export function binTick(
  span: HistoryBinSpan,
  unit: ChartUnit,
  window: HistoryWindow,
) {
  const start = new Date(span.since);
  if (unit === "hour")
    return start.getHours() % 6 === 0 && start.getMinutes() === 0
      ? timeOfDay(start)
      : undefined;
  if (unit === "day") {
    if (window.days <= 7) return weekdayDate.format(start).replace(",", "");
    return start.getDay() === 1 ? dayMonth.format(start) : undefined;
  }
  for (
    let day = startOfDay(span.since);
    day.getTime() < span.until;
    day = addDays(day, 1)
  )
    if (day.getDate() === 1) return monthName.format(day);
  return undefined;
}

/** The bin `day` falls in; -1 when none does. */
export function binIndexOf(spans: readonly HistoryBinSpan[], day: Date | null) {
  if (!day) return -1;
  const at = day.getTime();
  return spans.findIndex((span) => at >= span.since && at < span.until);
}

/**
 * Where a bin leads: a day's pull requests, a week as a 7-day list ending on
 * its last day, or an hour's day.
 */
export function binTarget(
  query: HistoryQuery,
  span: HistoryBinSpan,
  unit: ChartUnit,
  now: number,
): HistoryQuery {
  if (unit === "day")
    return { ...query, view: "list", day: formatDay(startOfDay(span.since)) };
  if (unit === "week") {
    const last = addDays(startOfDay(span.until), -1);
    const today = startOfDay(now);
    return {
      ...query,
      view: "list",
      range: "7d",
      end: last < today ? formatDay(last) : "",
      day: "",
    };
  }
  return { ...query, view: "list", day: "" };
}

const counts = new Intl.NumberFormat("en-GB");

export type ReadoutCell = { value: string; label: string };

/** The overview's totals: merged, opened, repositories, median to merge, still open. */
export function readoutCells(overview: HistoryOverviewData): ReadoutCell[] {
  const repositories = overview.repositories.length;
  return [
    { value: counts.format(overview.merged), label: "merged" },
    { value: counts.format(overview.opened), label: "opened" },
    {
      value: counts.format(repositories),
      label: repositories === 1 ? "repo" : "repos",
    },
    {
      value:
        overview.medianToMerge === null
          ? "–"
          : formatDuration(overview.medianToMerge),
      label: "median to merge",
    },
    { value: counts.format(overview.openNow), label: "still open" },
  ];
}

/** Escapes characters GitHub Markdown would read as formatting or HTML. */
export function escapeMarkdown(text: string) {
  return text.replace(/[\\[\]*_`<>]/g, (character) => `\\${character}`);
}

export const stateLabels: Record<HistoryPullState, string> = {
  open: "Open",
  draft: "Draft",
  closed: "Closed",
  merged: "Merged",
};

/** The list's filters as recap labels; the day already names the window. */
export function recapFilters(query: HistoryQuery) {
  return [
    query.activity === "merged"
      ? "Merged"
      : query.activity === "opened"
        ? "Opened"
        : "",
    query.state ? `${stateLabels[query.state]} now` : "",
    query.repo,
  ].filter(Boolean);
}

/**
 * The list as GitHub Markdown for a standup or self-review: a header naming
 * the window and filters, the summary sentence, then each repository (most
 * pull requests first) with one bullet per pull request.
 */
export function historyRecap(
  pulls: readonly HistoryPullRequest[],
  recap: {
    window: string;
    filters: readonly string[];
    summary: string;
    /** The cap when more pull requests matched than the recap holds. */
    capped: number | null;
  },
) {
  const repositories = new Map<string, HistoryPullRequest[]>();
  for (const pull of pulls)
    repositories.set(pull.fullName, [
      ...(repositories.get(pull.fullName) ?? []),
      pull,
    ]);
  const ordered = [...repositories].toSorted(
    (a, b) => b[1].length - a[1].length,
  );
  const lines = [
    `**${["Pull requests", recap.window, ...recap.filters].map(escapeMarkdown).join(" · ")}**`,
    "",
    recap.summary,
  ];
  for (const [repository, list] of ordered) {
    lines.push("", `**${escapeMarkdown(repository)}**`);
    for (const pull of list) {
      const link = `[#${pull.number} ${escapeMarkdown(pull.title)}](${pull.url})`;
      if (pull.merged) {
        const duration = openToMerge(pull);
        lines.push(
          duration === null
            ? `- Merged ${link}`
            : `- Merged ${link} · ${formatDuration(duration)} to merge`,
        );
      } else
        lines.push(
          pullState(pull) === "draft"
            ? `- Opened ${link}, draft, still open`
            : `- Opened ${link}`,
        );
    }
  }
  if (recap.capped !== null)
    lines.push("", `(latest ${counts.format(recap.capped)} pull requests)`);
  return lines.join("\n");
}
