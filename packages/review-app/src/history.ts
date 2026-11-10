import type {
  ApiGitHubHistory as HistoryData,
  ApiGitHubHistoryPullRequest as HistoryPullRequest,
} from "@diffx/api";

export type { HistoryData, HistoryPullRequest };

export const historyRanges = [
  { value: "today", label: "Today", days: 1 },
  { value: "7d", label: "7 days", days: 7 },
  { value: "30d", label: "30 days", days: 30 },
] as const;

export type HistoryRange = (typeof historyRanges)[number]["value"];
export type HistoryActivity = "" | "opened" | "merged";

/**
 * History's URL state: a range of whole local days ending on `end`
 * (YYYY-MM-DD; empty for today), the activity filter and a repository.
 */
export type HistoryQuery = {
  range: HistoryRange;
  end: string;
  activity: HistoryActivity;
  repo: string;
};

export const defaultHistoryQuery: HistoryQuery = {
  range: "7d",
  end: "",
  activity: "",
  repo: "",
};

export const historyRoute = "/history";

function rangeOf(value: string | null) {
  return historyRanges.find((range) => range.value === value);
}

function activityOf(value: string | null): HistoryActivity {
  return value === "opened" || value === "merged" ? value : "";
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
  return {
    range: rangeOf(params.get("range"))?.value ?? defaultHistoryQuery.range,
    end: parseDay(end) ? end : "",
    activity: activityOf(params.get("activity")),
    repo: params.get("repo") ?? "",
  };
}

export function historyPath(query: HistoryQuery) {
  const params = new URLSearchParams();
  if (query.range !== defaultHistoryQuery.range)
    params.set("range", query.range);
  if (query.end) params.set("end", query.end);
  if (query.activity) params.set("activity", query.activity);
  if (query.repo) params.set("repo", query.repo);
  const search = params.toString();
  return search ? `${historyRoute}?${search}` : historyRoute;
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
 * reaching today drops `end`.
 */
export function stepHistory(
  query: HistoryQuery,
  direction: -1 | 1,
  now: number,
): HistoryQuery {
  const window = historyWindow(query, now);
  const last = addDays(window.last, direction * window.days);
  const today = startOfDay(now);
  return { ...query, end: last < today ? formatDay(last) : "" };
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
const timeOfDay = new Intl.DateTimeFormat("en-GB", {
  hour: "2-digit",
  minute: "2-digit",
});

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
  return timeOfDay.format(eventTime(pull));
}

export function matchesHistory(pull: HistoryPullRequest, query: HistoryQuery) {
  if (query.repo && pull.fullName !== query.repo) return false;
  if (query.activity === "opened") return pull.opened;
  if (query.activity === "merged") return pull.merged;
  return true;
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

/** Activity counts, within the repository filter. */
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
