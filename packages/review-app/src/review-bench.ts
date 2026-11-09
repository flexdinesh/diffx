import type { ApiContext } from "@diffx/api";
import { compareContextActivity } from "./context-activity.ts";
import {
  projectKey,
  pullRequestOf,
  streamHarness,
  streamKey,
  streamMatches,
  type SeenStreams,
} from "./project-nav.ts";
import { contextHasChanges, searchWords } from "./project-picker.ts";

// The Review bench lists every review stream's head with changes and says
// whether it needs the reader. Phase 1 derives attention from per-browser
// opened and viewed heads; the server owns neither yet.

/** Needs attention (unread work), every current review, or closed ones. */
export type BenchView = "attention" | "open" | "closed";

export const benchViews = [
  { value: "attention", label: "Needs attention" },
  { value: "open", label: "All open" },
  { value: "closed", label: "Closed" },
] satisfies { value: BenchView; label: string }[];

/**
 * Why a review is listed. New and updated heads are unread and need
 * attention; drafts wait until ready; closed reviews left current work.
 */
export type BenchState = "new" | "updated" | "viewed" | "draft" | "closed";

export const benchStateLabels: Record<BenchState, string> = {
  new: "New",
  updated: "Updated",
  viewed: "Viewed",
  draft: "Draft",
  closed: "Closed",
};

/** Heads the reader opened, and heads marked viewed without opening. */
export type BenchMarks = { opened: SeenStreams; viewed: SeenStreams };

export type BenchQuery = {
  view: BenchView;
  /** A repository's `projectKey`, or empty for every repository. */
  repo: string;
  /** An agent harness, or empty for every source. */
  agent: string;
  q: string;
};

export type BenchRow = {
  id: string;
  context: ApiContext;
  state: BenchState;
  unread: boolean;
};

export type BenchSection = {
  id: "unread" | "rest";
  title: string;
  rows: BenchRow[];
};

export type BenchFacet = { value: string; label: string; count: number };

const day = 24 * 60 * 60 * 1000;

function isBenchView(value: string | null): value is BenchView {
  return value === "attention" || value === "open" || value === "closed";
}

export function benchQuery(search: string): BenchQuery {
  const params = new URLSearchParams(search);
  const view = params.get("view");
  return {
    view: isBenchView(view) ? view : "attention",
    repo: params.get("repo") ?? "",
    agent: params.get("agent") ?? "",
    q: params.get("q") ?? "",
  };
}

/** The bench URL for `query`; defaults stay out of the address. */
export function benchPath(query: BenchQuery) {
  const params = new URLSearchParams();
  if (query.view !== "attention") params.set("view", query.view);
  if (query.repo) params.set("repo", query.repo);
  if (query.agent) params.set("agent", query.agent);
  if (query.q) params.set("q", query.q);
  const search = params.toString();
  return search ? `/?${search}` : "/";
}

export function benchState(
  context: ApiContext,
  marks: BenchMarks | null,
): BenchState {
  const pull = pullRequestOf(context);
  if (context.stale || pull?.state === "merged" || pull?.state === "closed")
    return "closed";
  if (pull?.draft) return "draft";
  // Until the browser's marks load nothing reads as unread.
  if (!marks) return "viewed";
  const stream = streamKey(context);
  const opened = marks.opened[stream];
  const viewed = marks.viewed[stream];
  if (opened === context.id || viewed === context.id) return "viewed";
  return opened === undefined && viewed === undefined ? "new" : "updated";
}

export function needsAttention(state: BenchState) {
  return state === "new" || state === "updated";
}

function inView(state: BenchState, view: BenchView) {
  if (view === "closed") return state === "closed";
  if (view === "open") return state !== "closed";
  return needsAttention(state);
}

/** Every listed review: stream heads with changes, most active first. */
export function benchRows(heads: ApiContext[], marks: BenchMarks | null) {
  return heads
    .filter(contextHasChanges)
    .sort(compareContextActivity)
    .map((context): BenchRow => {
      const state = benchState(context, marks);
      return {
        id: streamKey(context),
        context,
        state,
        unread: needsAttention(state),
      };
    });
}

function matches(row: BenchRow, query: BenchQuery) {
  const { context } = row;
  if (query.repo && projectKey(context) !== query.repo) return false;
  if (query.agent && streamHarness(context) !== query.agent) return false;
  if (!searchWords(query.q).length) return true;
  return streamMatches(
    { id: row.id, context, unseen: row.unread },
    query.q,
    context.name,
  );
}

/** The rows a query lists, in the order the bench and its stepper use. */
export function benchList(rows: BenchRow[], query: BenchQuery) {
  return rows.filter(
    (row) => inView(row.state, query.view) && matches(row, query),
  );
}

/** Unread rows first, then the rest; empty sections drop out. */
export function benchSections(
  list: BenchRow[],
  view: BenchView,
): BenchSection[] {
  const sections: BenchSection[] = [
    {
      id: "unread",
      title: "New and updated",
      rows: list.filter((row) => row.unread),
    },
    {
      id: "rest",
      title: view === "closed" ? "Closed" : "Everything else",
      rows: list.filter((row) => !row.unread),
    },
  ];
  return sections.filter((section) => section.rows.length);
}

/** Flattened section order: what arrow keys and the review stepper follow. */
export function benchOrder(sections: BenchSection[]) {
  return sections.flatMap((section) => section.rows);
}

export function viewCounts(rows: BenchRow[]): Record<BenchView, number> {
  return {
    attention: rows.filter((row) => inView(row.state, "attention")).length,
    open: rows.filter((row) => inView(row.state, "open")).length,
    closed: rows.filter((row) => inView(row.state, "closed")).length,
  };
}

/** Repositories in the current view, by count then name. */
export function repositoryFacets(
  rows: BenchRow[],
  view: BenchView,
): BenchFacet[] {
  const facets = new Map<string, BenchFacet>();
  for (const row of rows) {
    if (!inView(row.state, view)) continue;
    const value = projectKey(row.context);
    const facet = facets.get(value) ?? {
      value,
      label: row.context.name,
      count: 0,
    };
    facet.count++;
    facets.set(value, facet);
  }
  return sortFacets([...facets.values()]);
}

/** Agent harnesses in the current view; empty while no agent published. */
export function agentFacets(rows: BenchRow[], view: BenchView): BenchFacet[] {
  const facets = new Map<string, BenchFacet>();
  for (const row of rows) {
    if (!inView(row.state, view)) continue;
    const value = streamHarness(row.context);
    if (!value) continue;
    const facet = facets.get(value) ?? { value, label: value, count: 0 };
    facet.count++;
    facets.set(value, facet);
  }
  return sortFacets([...facets.values()]);
}

function sortFacets(facets: BenchFacet[]) {
  return facets.sort(
    (left, right) =>
      right.count - left.count || left.label.localeCompare(right.label),
  );
}

/**
 * First visit (or a new device) counts work idle for a day as already opened,
 * so recent arrivals stand out without a wall of unread history.
 */
export function baselineOpened(
  heads: ApiContext[],
  now: number,
  activity: (context: ApiContext) => number,
): SeenStreams {
  return Object.fromEntries(
    heads
      .filter((context) => now - activity(context) >= day)
      .map((context) => [streamKey(context), context.id]),
  );
}

/** The review before or after `stream` in bench order, if any. */
export function adjacentReview(
  order: string[],
  stream: string,
  step: 1 | -1,
): string | undefined {
  const index = order.indexOf(stream);
  if (index < 0) return undefined;
  return order[index + step];
}

/** Top-level directories a diff touches, by changed file count. */
export function changedAreas(paths: string[], limit = 5) {
  const areas = new Map<string, number>();
  for (const path of paths) {
    const parts = path.split("/");
    const area = parts.length > 2 ? parts.slice(0, 2).join("/") : parts[0];
    if (area) areas.set(area, (areas.get(area) ?? 0) + 1);
  }
  return [...areas.entries()]
    .map(([area, count]) => ({ area, count }))
    .sort(
      (left, right) =>
        right.count - left.count || left.area.localeCompare(right.area),
    )
    .slice(0, limit);
}
