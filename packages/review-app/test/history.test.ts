// Day math runs in local time; a zone with daylight saving exercises it.
process.env.TZ = "America/New_York";

import assert from "node:assert/strict";
import { test } from "node:test";
import type {
  ApiGitHubHistory,
  ApiGitHubHistoryOverview,
  ApiGitHubHistoryPullRequest,
} from "@diffx/api";
import {
  activeDay,
  availableRanges,
  binLabel,
  binSpans,
  binTarget,
  binTick,
  canStepBack,
  chartUnit,
  dayLabel,
  defaultHistoryQuery,
  escapeMarkdown,
  eventLabel,
  formatDuration,
  historyDays,
  historyEdges,
  historyFiltered,
  historyPath,
  historyQuery,
  historyRecap,
  historySummary,
  historyWindow,
  listSummary,
  listWindow,
  pullState,
  rangeChoices,
  readoutCells,
  recapFilters,
  retentionNote,
  stepHistory,
  windowLabel,
  windowPhrase,
  type HistoryQuery,
} from "../src/history.ts";

// Mon 10 Nov 2026, 15:30 local.
const now = new Date(2026, 10, 10, 15, 30).getTime();
const day = (month: number, date: number, year = 2026) =>
  new Date(year, month - 1, date).getTime();
const query = (patch: Partial<HistoryQuery> = {}): HistoryQuery => ({
  ...defaultHistoryQuery,
  ...patch,
});

function pull(
  patch: Partial<ApiGitHubHistoryPullRequest>,
): ApiGitHubHistoryPullRequest {
  return {
    repositoryId: 1,
    fullName: "octo/app",
    number: 1,
    title: "Change",
    url: "https://github.com/octo/app/pull/1",
    draft: false,
    state: "open",
    createdAt: day(11, 9),
    mergedAt: null,
    closedAt: null,
    opened: true,
    merged: false,
    ...patch,
  };
}

test("query round-trips through the URL and drops invalid values", () => {
  const full = query({
    view: "list",
    range: "90d",
    end: "2026-11-03",
    activity: "merged",
    state: "draft",
    repo: "octo/app",
    day: "2026-10-20",
  });
  assert.equal(
    historyPath(full),
    "/history?view=list&range=90d&end=2026-11-03&activity=merged&state=draft&repo=octo%2Fapp&day=2026-10-20",
  );
  assert.deepEqual(historyQuery(historyPath(full).split("?")[1] ?? ""), full);
  assert.deepEqual(historyQuery("?range=1y"), query({ range: "1y" }));
  // The overview is the default view, so its URL omits the view.
  assert.equal(historyPath(defaultHistoryQuery), "/history");
  assert.equal(defaultHistoryQuery.view, "overview");
  assert.equal(historyPath(query({ view: "list" })), "/history?view=list");
  assert.deepEqual(
    historyQuery(
      "?view=grid&range=2y&end=2026-02-30&activity=closed&state=gone&day=2026-13-01",
    ),
    defaultHistoryQuery,
  );
});

test("longer ranges show only when retention keeps them", () => {
  const values = (retention: number | null) =>
    availableRanges(retention).map((range) => range.value);
  assert.deepEqual(values(null), ["today", "7d", "30d"]);
  assert.deepEqual(values(30), ["today", "7d", "30d"]);
  assert.deepEqual(values(89), ["today", "7d", "30d"]);
  assert.deepEqual(values(90), ["today", "7d", "30d", "90d"]);
  assert.deepEqual(values(363), ["today", "7d", "30d", "90d"]);
  assert.deepEqual(values(364), ["today", "7d", "30d", "90d", "1y"]);
  // A typed range beyond retention stays offered while chosen.
  assert.deepEqual(
    rangeChoices(30, "1y").map((range) => range.value),
    ["today", "7d", "30d", "1y"],
  );
  assert.equal(historyWindow(query({ range: "1y" }), now).days, 364);
});

test("windows are whole local days ending today or on end", () => {
  const week = historyWindow(query(), now);
  assert.equal(week.since, day(11, 4));
  assert.equal(week.until, day(11, 11));
  assert.equal(week.current, true);
  const today = historyWindow(query({ range: "today" }), now);
  assert.equal(today.since, day(11, 10));
  assert.equal(today.until, day(11, 11));
  const past = historyWindow(query({ range: "30d", end: "2026-10-05" }), now);
  assert.equal(past.since, day(9, 6));
  assert.equal(past.until, day(10, 6));
  assert.equal(past.current, false);
  // An end after today is today.
  assert.equal(
    historyWindow(query({ end: "2026-12-01" }), now).until,
    day(11, 11),
  );
});

test("windows span daylight saving changes by calendar days", () => {
  // 1 Nov 2026 ends daylight saving in New York: that day lasts 25 hours.
  const window = historyWindow(query({ end: "2026-11-03" }), now);
  assert.equal(window.since, day(10, 28));
  assert.equal(window.until, day(11, 4));
  assert.equal(window.until - window.since, 7 * 86_400_000 + 3_600_000);
});

test("stepping moves whole windows and stops at today", () => {
  const back = stepHistory(query(), -1, now);
  assert.equal(back.end, "2026-11-03");
  assert.equal(stepHistory(back, -1, now).end, "2026-10-27");
  assert.equal(stepHistory(back, 1, now).end, "");
  assert.equal(
    stepHistory(query({ range: "today" }), -1, now).end,
    "2026-11-09",
  );
  assert.equal(
    stepHistory(query({ range: "30d", end: "2026-10-01" }), 1, now).end,
    "2026-10-31",
  );
});

test("stepping back needs the previous window to overlap retained history", () => {
  const retained = day(10, 11) + 15 * 3_600_000;
  assert.equal(canStepBack(historyWindow(query(), now), retained), true);
  const oldest = historyWindow(query({ end: "2026-10-13" }), now);
  assert.equal(canStepBack(oldest, retained), false);
  assert.equal(
    retentionNote(oldest, retained),
    "History before 12 Oct isn't kept.",
  );
  assert.equal(retentionNote(historyWindow(query(), now), retained), "");
});

test("labels and phrases name days and ranges", () => {
  assert.equal(dayLabel(new Date(day(11, 10)), now), "Today");
  assert.equal(dayLabel(new Date(day(11, 9)), now), "Yesterday");
  assert.equal(dayLabel(new Date(day(11, 7)), now), "Sat 7 Nov");
  assert.equal(dayLabel(new Date(day(12, 31, 2025)), now), "Wed 31 Dec 2025");
  const week = historyWindow(query(), now);
  assert.equal(windowLabel(week, now), "4 – 10 Nov");
  assert.equal(windowPhrase(week, now), "in the last 7 days");
  const across = historyWindow(query({ end: "2026-11-03" }), now);
  assert.equal(windowLabel(across, now), "28 Oct – 3 Nov");
  assert.equal(windowPhrase(across, now), "from 28 Oct to 3 Nov");
  const yesterday = historyWindow(
    query({ range: "today", end: "2026-11-09" }),
    now,
  );
  assert.equal(windowPhrase(yesterday, now), "yesterday");
  assert.equal(
    windowPhrase(historyWindow(query({ range: "today" }), now), now),
    "today",
  );
});

test("summary counts each activity once", () => {
  assert.equal(
    historySummary(12, 9, 4, "in the last 7 days"),
    "You merged 9 and opened 12 pull requests across 4 repositories in the last 7 days.",
  );
  assert.equal(
    historySummary(1, 0, 1, "today"),
    "You opened 1 pull request today.",
  );
  assert.equal(
    historySummary(0, 2, 1, "yesterday"),
    "You merged 2 pull requests yesterday.",
  );
});

test("rows group by their latest in-range event day", () => {
  const openedAndMerged = pull({
    number: 1,
    createdAt: day(11, 8) + 3_600_000,
    mergedAt: day(11, 10) + 3_600_000,
    state: "merged",
    merged: true,
  });
  const mergedOnly = pull({
    number: 2,
    createdAt: day(10, 1),
    mergedAt: day(11, 9) + 7_200_000,
    state: "merged",
    opened: false,
    merged: true,
  });
  const openedOnly = pull({ number: 3, createdAt: day(11, 10) + 600_000 });
  const days = historyDays([mergedOnly, openedOnly, openedAndMerged], now);
  assert.deepEqual(
    days.map((group) => [group.title, group.pulls.map((item) => item.number)]),
    [
      ["Today", [1, 3]],
      ["Yesterday", [2]],
    ],
  );
  assert.equal(eventLabel(openedAndMerged), "Opened · Merged");
  assert.equal(eventLabel(mergedOnly), "Merged");
  assert.equal(eventLabel(openedOnly), "Opened");
});

test("today charts absolute hours, 23 or 25 across daylight saving", () => {
  const hours = (end: string) =>
    historyEdges(historyWindow(query({ range: "today", end }), now), "hour");
  // 8 Mar 2026 starts daylight saving in New York; 1 Nov ends it.
  const spring = hours("2026-03-08");
  const autumn = hours("2026-11-01");
  assert.equal(binSpans(spring).length, 23);
  assert.equal(binSpans(autumn).length, 25);
  assert.equal(binSpans(hours("2026-11-02")).length, 24);
  for (const edges of [spring, autumn])
    for (const span of binSpans(edges))
      assert.equal(span.until - span.since, 3_600_000);
  assert.equal(spring[0], day(3, 8));
  assert.equal(spring.at(-1), day(3, 9));
  assert.equal(autumn.at(-1), day(11, 2));
  const window = historyWindow(
    query({ range: "today", end: "2026-11-01" }),
    now,
  );
  const ticks = binSpans(autumn)
    .map((span) => binTick(span, "hour", window))
    .filter(Boolean);
  assert.deepEqual(ticks, ["00:00", "06:00", "12:00", "18:00"]);
  const first = binSpans(autumn)[6];
  assert.ok(first);
  assert.equal(binLabel(first, "hour", now), "05:00 – 06:00");
});

test("daily bins are local midnights across daylight saving", () => {
  const window = historyWindow(query({ end: "2026-11-03" }), now);
  const edges = historyEdges(window, "day");
  assert.deepEqual(
    edges,
    Array.from({ length: 8 }, (_, index) => day(10, 28 + index)),
  );
  assert.deepEqual(
    binSpans(edges).map((span) => binTick(span, "day", window)),
    ["Wed 28", "Thu 29", "Fri 30", "Sat 31", "Sun 1", "Mon 2", "Tue 3"],
  );
  assert.equal(
    binLabel({ since: day(11, 9), until: day(11, 10) }, "day", now),
    "Yesterday",
  );
  // Longer ranges label Mondays only.
  const month = historyWindow(query({ range: "30d" }), now);
  const ticks = binSpans(historyEdges(month, "day"))
    .map((span) => binTick(span, "day", month))
    .filter(Boolean);
  assert.deepEqual(ticks, ["12 Oct", "19 Oct", "26 Oct", "2 Nov", "9 Nov"]);
});

test("chart units follow the range and compact screens", () => {
  assert.equal(chartUnit("today", false), "hour");
  assert.equal(chartUnit("7d", true), "day");
  assert.equal(chartUnit("30d", true), "day");
  assert.equal(chartUnit("90d", false), "day");
  assert.equal(chartUnit("90d", true), "week");
  assert.equal(chartUnit("1y", false), "week");
  assert.equal(
    historyEdges(historyWindow(query({ range: "90d" }), now), "day").length,
    91,
  );
});

test("compact 90 days charts 13 weeks counted back from the end", () => {
  const window = historyWindow(query({ range: "90d" }), now);
  const edges = historyEdges(window, "week");
  assert.equal(edges.length, 14);
  assert.equal(edges[0], window.since);
  assert.equal(edges.at(-1), window.until);
  // 90 days are 12 whole weeks and a partial first block of 6 days.
  assert.equal(edges[1], day(11, 11 - 84));
  for (let index = 1; index < 13; index++)
    assert.equal(edges[index + 1], day(11, 11 - 84 + 7 * index));
  assert.equal(
    binLabel({ since: day(11, 4), until: day(11, 11) }, "week", now),
    "4 – 10 Nov",
  );
});

test("a year charts 52 weekly blocks ending on its last day", () => {
  const window = historyWindow(query({ range: "1y" }), now);
  assert.equal(window.since, day(11, 10 - 363));
  const edges = historyEdges(window, "week");
  assert.equal(binSpans(edges).length, 52);
  assert.equal(edges[0], window.since);
  assert.equal(edges.at(-1), window.until);
  const ticks = binSpans(edges)
    .map((span) => binTick(span, "week", window))
    .filter(Boolean);
  assert.equal(ticks.length, 12);
  assert.equal(ticks[0], "Dec");
  assert.equal(ticks.at(-1), "Nov");
});

test("bins lead to their pull requests", () => {
  const overview = query({ range: "30d", repo: "octo/app" });
  assert.deepEqual(
    binTarget(overview, { since: day(11, 8), until: day(11, 9) }, "day", now),
    { ...overview, view: "list", day: "2026-11-08" },
  );
  const year = query({ range: "1y" });
  const spans = binSpans(historyEdges(historyWindow(year, now), "week"));
  const last = spans.at(-1);
  const previous = spans.at(-2);
  assert.ok(last && previous);
  assert.deepEqual(binTarget(year, last, "week", now), {
    ...year,
    view: "list",
    range: "7d",
    end: "",
    day: "",
  });
  assert.equal(binTarget(year, previous, "week", now).end, "2026-11-03");
  const today = query({ range: "today", day: "2026-11-10" });
  assert.deepEqual(
    binTarget(
      today,
      { since: day(11, 10), until: day(11, 10) + 3_600_000 },
      "hour",
      now,
    ),
    { ...today, view: "list", day: "" },
  );
});

test("the list narrows to a day in range and ignores others", () => {
  const chosen = query({ view: "list", day: "2026-11-08" });
  const window = listWindow(chosen, now);
  assert.equal(window.since, day(11, 8));
  assert.equal(window.until, day(11, 9));
  assert.equal(windowLabel(window, now), "Sun 8 Nov");
  assert.equal(windowPhrase(window, now), "on Sun 8 Nov");
  assert.equal(historyFiltered(chosen, now), true);
  const outside = query({ view: "list", day: "2026-10-01" });
  assert.equal(activeDay(outside, now), null);
  assert.deepEqual(listWindow(outside, now), historyWindow(outside, now));
  assert.equal(historyFiltered(outside, now), false);
  // Stepping or changing the window leaves the day behind.
  assert.equal(stepHistory(chosen, -1, now).day, "");
});

test("pull requests have one state now", () => {
  assert.equal(pullState(pull({ state: "merged" })), "merged");
  assert.equal(pullState(pull({ state: "closed", draft: true })), "closed");
  assert.equal(pullState(pull({ draft: true })), "draft");
  assert.equal(pullState(pull({})), "open");
});

test("durations read in their two largest units", () => {
  const minute = 60_000;
  assert.equal(formatDuration(30_000), "<1m");
  assert.equal(formatDuration(42 * minute), "42m");
  assert.equal(formatDuration(5 * 60 * minute + 12 * minute), "5h 12m");
  assert.equal(formatDuration(5 * 60 * minute), "5h");
  assert.equal(formatDuration(51 * 60 * minute + 20 * minute), "2d 3h");
  assert.equal(formatDuration(48 * 60 * minute), "2d");
});

function overviewOf(
  patch: Partial<ApiGitHubHistoryOverview>,
): ApiGitHubHistoryOverview {
  return {
    connected: true,
    following: true,
    retainedSince: day(10, 11),
    since: day(11, 4),
    until: day(11, 11),
    opened: 5,
    merged: 3,
    openNow: 2,
    medianToMerge: null,
    bins: [],
    repositories: [
      {
        fullName: "octo/app",
        opened: 5,
        merged: 3,
        openNow: 2,
        medianToMerge: null,
        merges: [],
      },
    ],
    stillOpen: [],
    truncated: false,
    ...patch,
  };
}

test("readouts count repositories and name a missing median", () => {
  assert.deepEqual(readoutCells(overviewOf({})), [
    { value: "3", label: "merged" },
    { value: "5", label: "opened" },
    { value: "1", label: "repo" },
    { value: "–", label: "median to merge" },
    { value: "2", label: "still open" },
  ]);
  const busy = readoutCells(
    overviewOf({
      merged: 1234,
      medianToMerge: 5 * 3_600_000 + 12 * 60_000,
      repositories: [
        {
          fullName: "octo/app",
          opened: 1,
          merged: 1,
          openNow: 0,
          medianToMerge: null,
          merges: [],
        },
        {
          fullName: "octo/api",
          opened: 1,
          merged: 1,
          openNow: 0,
          medianToMerge: null,
          merges: [],
        },
      ],
    }),
  );
  assert.deepEqual(busy[0], { value: "1,234", label: "merged" });
  assert.deepEqual(busy[2], { value: "2", label: "repos" });
  assert.deepEqual(busy[3], { value: "5h 12m", label: "median to merge" });
});

test("the list summary uses window totals within filters", () => {
  const data: ApiGitHubHistory = {
    connected: true,
    following: true,
    retainedSince: day(10, 11),
    since: day(11, 4),
    until: day(11, 11),
    opened: 12,
    merged: 9,
    repositories: [
      { fullName: "octo/app", opened: 8, merged: 9 },
      { fullName: "octo/api", opened: 4, merged: 0 },
    ],
    states: { open: 2, draft: 1, closed: 0, merged: 9 },
    pullRequests: [],
    nextCursor: null,
  };
  const week = historyWindow(query(), now);
  assert.equal(
    listSummary(data, query(), week, now),
    "You merged 9 and opened 12 pull requests across 2 repositories in the last 7 days.",
  );
  assert.equal(
    listSummary(data, query({ activity: "merged" }), week, now),
    "You merged 9 pull requests in the last 7 days.",
  );
  assert.equal(
    listSummary(data, query({ repo: "octo/api" }), week, now),
    "You opened 4 pull requests in the last 7 days.",
  );
});

test("the recap groups pull requests by repository as escaped Markdown", () => {
  const hour = 3_600_000;
  const pulls = [
    pull({
      fullName: "octo/api",
      number: 4,
      title: "Fix *bold* `code` <tag> [link] a_b \\ end",
      url: "https://github.com/octo/api/pull/4",
      state: "merged",
      createdAt: day(11, 9),
      mergedAt: day(11, 9) + 5 * hour + 12 * 60_000,
      opened: true,
      merged: true,
    }),
    pull({
      number: 9,
      title: "Draft search",
      url: "https://github.com/octo/app/pull/9",
      draft: true,
    }),
    pull({
      number: 7,
      title: "Add search",
      url: "https://github.com/octo/app/pull/7",
      state: "merged",
      createdAt: day(11, 7),
      mergedAt: day(11, 9) + hour,
      opened: false,
      merged: true,
    }),
  ];
  const chosen = query({
    view: "list",
    activity: "merged",
    state: "merged",
    repo: "octo/app",
  });
  assert.deepEqual(recapFilters(chosen), ["Merged", "Merged now", "octo/app"]);
  assert.equal(
    historyRecap(pulls, {
      window: "4 – 10 Nov",
      filters: [],
      summary:
        "You merged 2 and opened 2 pull requests across 2 repositories in the last 7 days.",
      capped: null,
    }),
    [
      "**Pull requests · 4 – 10 Nov**",
      "",
      "You merged 2 and opened 2 pull requests across 2 repositories in the last 7 days.",
      "",
      "**octo/app**",
      "- Opened [#9 Draft search](https://github.com/octo/app/pull/9), draft, still open",
      "- Merged [#7 Add search](https://github.com/octo/app/pull/7) · 2d 1h to merge",
      "",
      "**octo/api**",
      "- Merged [#4 Fix \\*bold\\* \\`code\\` \\<tag\\> \\[link\\] a\\_b \\\\ end](https://github.com/octo/api/pull/4) · 5h 12m to merge",
    ].join("\n"),
  );
  const capped = historyRecap(pulls.slice(1), {
    window: "Sun 8 Nov",
    filters: ["Opened", "octo/app"],
    summary: "You opened 2 pull requests on Sun 8 Nov.",
    capped: 1000,
  });
  assert.ok(
    capped.startsWith("**Pull requests · Sun 8 Nov · Opened · octo/app**\n"),
  );
  assert.ok(capped.endsWith("\n\n(latest 1,000 pull requests)"));
  assert.equal(escapeMarkdown("a_b"), "a\\_b");
});
