import type {
  CodeViewItem,
  FileDiffMetadata,
  SelectionSide,
} from "@pierre/diffs";

/** Unchanged lines kept above a change when it scrolls to the top. */
export const CHANGE_CONTEXT_LINES = 3;

export interface LineSpan {
  start: number;
  end: number;
}

/** One contiguous block of changed lines: a change navigation stop. */
export interface ChangeStop {
  path: string;
  /** Position within its file, from 0. */
  index: number;
  /** Change blocks in its file. */
  count: number;
  deletions: LineSpan | null;
  additions: LineSpan | null;
  /** The line aligned below the sticky header when the stop scrolls into view. */
  anchor: { side: SelectionSide; lineNumber: number };
}

export interface StopKey {
  path: string;
  index: number;
}

function fileStops(path: string, diff: FileDiffMetadata): ChangeStop[] {
  const stops: Omit<ChangeStop, "count">[] = [];
  for (const hunk of diff.hunks) {
    // Line numbers of the next deleted and added lines.
    let deletion = hunk.deletionStart;
    let addition = hunk.additionStart;
    let context = 0;
    for (const content of hunk.hunkContent) {
      if (content.type === "context") {
        deletion += content.lines;
        addition += content.lines;
        context = content.lines;
        continue;
      }
      const deletions =
        content.deletions > 0
          ? { start: deletion, end: deletion + content.deletions - 1 }
          : null;
      const additions =
        content.additions > 0
          ? { start: addition, end: addition + content.additions - 1 }
          : null;
      // Unified layouts list deletions first, so blocks start there.
      const lead = Math.min(CHANGE_CONTEXT_LINES, context);
      const anchor: ChangeStop["anchor"] =
        lead > 0
          ? { side: "additions", lineNumber: addition - lead }
          : deletions
            ? { side: "deletions", lineNumber: deletions.start }
            : { side: "additions", lineNumber: addition };
      if (deletions || additions)
        stops.push({
          path,
          index: stops.length,
          deletions,
          additions,
          anchor,
        });
      deletion += content.deletions;
      addition += content.additions;
      context = 0;
    }
  }
  return stops.map((stop) => ({ ...stop, count: stops.length }));
}

/**
 * Change blocks of the visible, expanded files in viewer order. Collapsed
 * files and placeholder items (loading, not loaded, binary) have none.
 */
export function changeStops<LAnnotation>(
  paths: readonly string[],
  items: ReadonlyMap<string, CodeViewItem<LAnnotation>>,
  collapsed: ReadonlySet<string>,
): ChangeStop[] {
  return paths.flatMap((path) => {
    const item = items.get(path);
    return item?.type === "diff" && !collapsed.has(path)
      ? fileStops(path, item.fileDiff)
      : [];
  });
}

/** A stop's rows in scroll coordinates. */
export interface StopRect {
  top: number;
  bottom: number;
}

export interface ChangeViewport {
  /** Below the sticky file header. */
  top: number;
  bottom: number;
}

/**
 * The item one step from the current one, by index. The last item navigated
 * to stays current while any of it is on screen; otherwise the viewport's top
 * decides, so manual scrolling restarts from what is visible. Null at either
 * end. Change and file navigation share this rule.
 */
export function stepInViewport<T>(
  items: readonly T[],
  delta: -1 | 1,
  viewport: ChangeViewport,
  rect: (item: T) => StopRect,
  current: number,
): T | null {
  const shown = current >= 0 ? items[current] : undefined;
  if (shown) {
    const { top, bottom } = rect(shown);
    if (bottom > viewport.top && top < viewport.bottom)
      return items[current + delta] ?? null;
  }
  if (delta === 1)
    return items.find((item) => rect(item).top >= viewport.top) ?? null;
  return items.findLast((item) => rect(item).top < viewport.top) ?? null;
}

/** The change stop one step from the cursor; see `stepInViewport`. */
export function stepChangeStop(
  stops: readonly ChangeStop[],
  delta: -1 | 1,
  viewport: ChangeViewport,
  rect: (stop: ChangeStop) => StopRect,
  cursor: StopKey | null,
): ChangeStop | null {
  const current = cursor
    ? stops.findIndex(
        (stop) => stop.path === cursor.path && stop.index === cursor.index,
      )
    : -1;
  return stepInViewport(stops, delta, viewport, rect, current);
}
