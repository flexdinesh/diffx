import {
  BarChart,
  BarChartLegend,
  type BarChartBin,
  type BarChartSeries,
} from "@diffx/ui/components/bar-chart";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { ListRow } from "@diffx/ui/components/list-row";
import { ReadoutStrip } from "@diffx/ui/components/readout-strip";
import { Sparkline } from "@diffx/ui/components/sparkline";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import { formatCount } from "@diffx/ui/lib/format";
import {
  activeDay,
  binIndexOf,
  binLabel,
  binSpans,
  binTarget,
  binTick,
  formatDuration,
  historyPath,
  readoutCells,
  retentionNote,
  windowLabel,
  type ChartUnit,
  type HistoryOverviewData,
  type HistoryQuery,
  type HistoryWindow,
} from "./history.ts";
import { PullIcon } from "./HistoryParts.tsx";
import { streamClick } from "./StreamParts.tsx";

const series: BarChartSeries[] = [
  { name: "merged", mark: "solid" },
  { name: "opened", mark: "hollow" },
];

/**
 * History's overview of a window: totals as a readout strip, merged and
 * opened pull requests per bin (a bin opens its pull requests), each
 * repository's figures with a merge sparkline, and pull requests still open.
 */
export function HistoryOverview({
  overview,
  query,
  span,
  unit,
  edges,
  now,
  onChange,
}: {
  overview: HistoryOverviewData;
  query: HistoryQuery;
  span: HistoryWindow;
  unit: ChartUnit;
  edges: readonly number[];
  now: number;
  onChange: (query: HistoryQuery) => void;
}) {
  const note = retentionNote(span, overview.retainedSince);
  const spans = binSpans(edges);
  const bins: BarChartBin[] = spans.map((bin, index) => {
    const counts = overview.bins[index];
    return {
      id: String(bin.since),
      label: binLabel(bin, unit, now),
      tick: binTick(bin, unit, span),
      values: [counts?.merged ?? 0, counts?.opened ?? 0],
      disabled: bin.since >= now,
      unavailable: bin.until <= overview.retainedSince,
    };
  });
  const selected =
    unit === "hour"
      ? undefined
      : spans[binIndexOf(spans, activeDay(query, now))];
  const current = spans.findIndex((bin) => now >= bin.since && now < bin.until);
  const nowBin = spans[current];
  const marker =
    unit === "hour" && nowBin
      ? {
          index: current,
          fraction: (now - nowBin.since) / (nowBin.until - nowBin.since),
          label: "Now",
        }
      : undefined;
  return (
    <>
      <div className="history-readout">
        <ReadoutStrip label="Activity summary" cells={readoutCells(overview)} />
      </div>
      {note && <p className="history-note">{note}</p>}
      <section aria-labelledby="history-activity">
        <div className="bench-section">
          <h2 id="history-activity">Activity</h2>
          <div className="ml-auto">
            <BarChartLegend series={series} />
          </div>
        </div>
        <div className="history-chart">
          <BarChart
            label={`Activity, ${windowLabel(span, now)}`}
            series={series}
            bins={bins}
            selected={selected && String(selected.since)}
            marker={marker}
            onActivate={(id) => {
              const bin = spans.find((item) => String(item.since) === id);
              if (bin) onChange(binTarget(query, bin, unit, now));
            }}
          />
        </div>
      </section>
      <HistoryRepositories
        overview={overview}
        query={query}
        onChange={onChange}
      />
      <HistoryStillOpen overview={overview} now={now} />
    </>
  );
}

function HistoryRepositories({
  overview,
  query,
  onChange,
}: {
  overview: HistoryOverviewData;
  query: HistoryQuery;
  onChange: (query: HistoryQuery) => void;
}) {
  if (!overview.repositories.length) return null;
  // One scale across rows, so sparklines compare.
  const max = Math.max(
    1,
    ...overview.repositories.flatMap((repository) => repository.merges),
  );
  return (
    <section aria-labelledby="history-repositories">
      <div className="bench-section">
        <h2 id="history-repositories">Repositories</h2>
        <CountBadge count={overview.repositories.length} />
      </div>
      <div className="history-repo-row history-columns" aria-hidden="true">
        <span>Repository</span>
        <span className="text-right">Merged</span>
        <span className="text-right">Opened</span>
        <span className="text-right">Open now</span>
        <span className="text-right">Median to merge</span>
        <span>Merges</span>
      </div>
      <ul className="bench-list" aria-label="Repositories">
        {overview.repositories.map((repository) => {
          const next: HistoryQuery = {
            ...query,
            view: "list",
            repo: repository.fullName,
          };
          const median =
            repository.medianToMerge === null
              ? null
              : formatDuration(repository.medianToMerge);
          const figures = [
            `${formatCount(repository.merged)} merged`,
            `${formatCount(repository.opened)} opened`,
            `${formatCount(repository.openNow)} open`,
          ];
          return (
            <li key={repository.fullName}>
              <ListRow
                variant="table"
                depth={1}
                className="history-repo-row"
                render={<a href={historyPath(next)} />}
                aria-current={
                  query.repo === repository.fullName ? "true" : undefined
                }
                aria-label={`${repository.fullName}: ${figures.join(", ")} now${
                  median ? `, ${median} median to merge` : ""
                }`}
                onClick={(event) => streamClick(event, () => onChange(next))}
              >
                <span className="min-w-0 truncate font-mono text-xs">
                  {repository.fullName}
                </span>
                <span className="history-figure">
                  {formatCount(repository.merged)}
                </span>
                <span className="history-figure">
                  {formatCount(repository.opened)}
                </span>
                <span className="history-figure">
                  {formatCount(repository.openNow)}
                </span>
                <span className="history-figure">{median ?? "–"}</span>
                <span className="history-spark">
                  <Sparkline values={repository.merges} max={max} />
                </span>
                <span className="history-repo-stack">
                  {[...figures, ...(median ? [median] : [])].join(" · ")}
                </span>
              </ListRow>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function HistoryStillOpen({
  overview,
  now,
}: {
  overview: HistoryOverviewData;
  now: number;
}) {
  if (!overview.stillOpen.length) return null;
  return (
    <section aria-labelledby="history-still-open">
      <div className="bench-section">
        <h2 id="history-still-open">Still open</h2>
        <CountBadge count={overview.openNow} />
      </div>
      <ul className="bench-list" aria-label="Still open">
        {overview.stillOpen.map((pull) => (
          <li key={`${pull.repositoryId}#${pull.number}`}>
            <ListRow
              variant="table"
              depth={1}
              className="history-open-row"
              render={
                <a href={pull.url} target="_blank" rel="noopener noreferrer" />
              }
              title={`${pull.fullName}#${pull.number} · ${pull.title} · Open on GitHub`}
            >
              <PullIcon state={pull.draft ? "draft" : "open"} />
              <span className="min-w-0 truncate">
                <span className="text-muted tabular-nums">#{pull.number}</span>{" "}
                {pull.title}
              </span>
              <span className="min-w-0 truncate font-mono text-xs text-secondary">
                {pull.fullName}
              </span>
              <span>{pull.draft && <Badge variant="status">Draft</Badge>}</span>
              <span className="text-right font-mono text-xs text-muted tabular-nums">
                open {formatDuration(Math.max(0, now - pull.createdAt))}
              </span>
            </ListRow>
          </li>
        ))}
      </ul>
      {overview.openNow > overview.stillOpen.length && (
        <p className="history-note history-note-after">
          Showing the oldest {overview.stillOpen.length} of{" "}
          {formatCount(overview.openNow)}.
        </p>
      )}
    </section>
  );
}

/** The overview's layout in placeholders while it loads. */
export function HistoryOverviewSkeleton() {
  return (
    <div role="status">
      <div className="history-readout">
        <div className="history-skeleton-strip">
          {Array.from({ length: 5 }, (_, index) => (
            <Skeleton key={index} shape="label" className="w-full" />
          ))}
        </div>
      </div>
      <div className="bench-section">
        <Skeleton shape="line" className="w-16" />
      </div>
      <div className="history-chart">
        <div className="history-skeleton-chart">
          <Skeleton className="h-full w-full" />
        </div>
      </div>
      <div className="bench-section">
        <Skeleton shape="line" className="w-24" />
      </div>
      <div className="flex flex-col gap-3 p-4">
        {Array.from({ length: 4 }, (_, index) => (
          <Skeleton key={index} shape="control" className="w-full" />
        ))}
      </div>
      <p className="visually-hidden">Loading overview…</p>
    </div>
  );
}
