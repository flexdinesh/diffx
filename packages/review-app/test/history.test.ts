// Day math runs in local time; a zone with daylight saving exercises it.
process.env.TZ = "America/New_York";

import assert from "node:assert/strict";
import { test } from "node:test";
import type { ApiGitHubHistoryPullRequest } from "@diffx/api";
import {
  canStepBack,
  dayLabel,
  defaultHistoryQuery,
  eventLabel,
  historyDays,
  historyPath,
  historyQuery,
  historySummary,
  historyWindow,
  matchesHistory,
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
    range: "30d",
    end: "2026-11-03",
    activity: "merged",
    repo: "octo/app",
  });
  assert.deepEqual(historyQuery(historyPath(full).split("?")[1] ?? ""), full);
  assert.equal(historyPath(defaultHistoryQuery), "/history");
  assert.deepEqual(
    historyQuery("?range=1y&end=2026-02-30&activity=closed"),
    defaultHistoryQuery,
  );
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

test("filters match activity and repository", () => {
  const merged = pull({ opened: false, merged: true, fullName: "octo/api" });
  assert.equal(matchesHistory(merged, query({ activity: "merged" })), true);
  assert.equal(matchesHistory(merged, query({ activity: "opened" })), false);
  assert.equal(matchesHistory(merged, query({ repo: "octo/app" })), false);
  assert.equal(matchesHistory(merged, query({ repo: "octo/api" })), true);
});
