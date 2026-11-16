import { useRef, useState, type KeyboardEvent } from "react";
import {
  createTooltipHandle,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@diffx/ui/components/ui/tooltip";
import { formatCount } from "@diffx/ui/lib/format";
import { cn } from "@diffx/ui/lib/utils";

export type BarChartMark = "solid" | "hollow";

export interface BarChartSeries {
  name: string;
  mark: BarChartMark;
}

export interface BarChartBin {
  /** Stable key. */
  id: string;
  /** Full label for the accessible name and tooltip, e.g. "Sat 8 Nov". */
  label: string;
  /** Short axis label under the bin, e.g. "Sat 8"; omit to leave it blank. */
  tick?: string | undefined;
  /** One value per series, in series order. */
  values: number[];
  /** Not yet reachable (future hours): no bars, not focusable or clickable. */
  disabled?: boolean | undefined;
  /** History not kept: a hatched band, not clickable. */
  unavailable?: boolean | undefined;
}

export interface BarChartProps {
  /** Group accessible name, e.g. "Activity, 4 – 10 Nov". */
  label: string;
  series: BarChartSeries[];
  bins: BarChartBin[];
  /** Bin id rendered with the accent. */
  selected?: string | undefined;
  /** A "now" line at bins[index] plus fraction (0..1) of its width. */
  marker?: { index: number; fraction: number; label: string } | undefined;
  /** Click, Enter or Space on a bin. */
  onActivate?: ((id: string) => void) | undefined;
  className?: string | undefined;
}

// Marks are monochrome ink: solid fills with secondary text color, hollow is a
// control-boundary outline. Only the selected bin switches to the accent.
const binMark: Record<BarChartMark, string> = {
  solid: "bg-secondary group-data-selected/bin:bg-accent",
  hollow: "border border-control group-data-selected/bin:border-accent",
};
const swatchMark: Record<BarChartMark, string> = {
  solid: "bg-secondary",
  hollow: "border border-control",
};

// One neutral column per bin; hover and focus share the hover surface, the
// selected bin keeps its selected surface.
const column =
  "group/bin relative flex h-full min-w-0 flex-1 items-end justify-center hover:bg-hover focus-visible:z-10 focus-visible:bg-hover disabled:hover:bg-transparent aria-disabled:hover:bg-transparent data-selected:bg-selected data-selected:hover:bg-selected data-selected:focus-visible:bg-selected";

/** Smallest round top at or above max with a whole-count midline: 3→4, 13→20, 47→60. */
function scaleTop(max: number): number {
  if (max <= 1) return 1;
  const magnitude = 10 ** Math.floor(Math.log10(max));
  for (const step of [1, 2, 4, 6, 8]) {
    if (step * magnitude >= max) return step * magnitude;
  }
  return 10 * magnitude;
}

function percent(value: number, top: number): string {
  return `${Math.min(100, Math.max(0, (value / top) * 100))}%`;
}

function binLabel(bin: BarChartBin, series: BarChartSeries[]): string {
  return `${bin.label}: ${series
    .map((entry, index) => `${bin.values[index] ?? 0} ${entry.name}`)
    .join(", ")}`;
}

/**
 * Paired activity bars per bin on a quiet grid. Bins are native buttons with
 * one roving tab stop (Arrow keys, Home, End); one shared tooltip repeats each
 * bin's accessible name. Bars are CSS columns so corners stay square and
 * colors stay on tokens.
 */
export function BarChart({
  label,
  series,
  bins,
  selected,
  marker,
  onActivate,
  className,
}: BarChartProps) {
  const [tooltip] = useState(() => createTooltipHandle<string>());
  const [active, setActive] = useState<string>();
  const buttons = useRef(new Map<string, HTMLElement>());

  const top = scaleTop(
    Math.max(
      0,
      ...bins.flatMap((bin) =>
        bin.disabled || bin.unavailable ? [] : bin.values,
      ),
    ),
  );
  const grid = top === 1 ? [0, 1] : [0, top / 2, top];
  const focusable = bins.filter((bin) => !bin.disabled && !bin.unavailable);
  const stop = focusable.some((bin) => bin.id === active)
    ? active
    : (focusable.find((bin) => bin.id === selected) ?? focusable[0])?.id;
  const ticks = bins.some((bin) => bin.tick);
  const markerLeft =
    marker && bins.length > 0
      ? percent(
          Math.min(Math.max(marker.index, 0), bins.length - 1) +
            Math.min(Math.max(marker.fraction, 0), 1),
          bins.length,
        )
      : undefined;

  function move(event: KeyboardEvent<HTMLDivElement>) {
    const current = focusable.findIndex((bin) => bin.id === stop);
    const last = focusable.length - 1;
    const next =
      event.key === "ArrowRight"
        ? Math.min(current + 1, last)
        : event.key === "ArrowLeft"
          ? Math.max(current - 1, 0)
          : event.key === "Home"
            ? 0
            : event.key === "End"
              ? last
              : undefined;
    if (next === undefined) return;
    event.preventDefault();
    const bin = focusable[next];
    if (!bin) return;
    setActive(bin.id);
    buttons.current.get(bin.id)?.focus();
  }

  return (
    <div
      data-slot="bar-chart"
      className={cn(
        "grid min-w-0 grid-cols-[minmax(0,1fr)_auto] gap-1",
        className,
      )}
    >
      <div className="relative h-chart min-w-0">
        {grid.map((value) => (
          <span
            key={value}
            aria-hidden="true"
            className={cn(
              "pointer-events-none absolute inset-x-0 border-t",
              value === 0 ? "border-border" : "border-dashed border-separator",
            )}
            style={{ top: percent(top - value, top) }}
          />
        ))}
        <div
          role="group"
          aria-label={label}
          className="relative flex h-full min-w-0"
          onKeyDown={move}
        >
          {bins.map((bin) => {
            const isSelected = bin.id === selected;
            const name = bin.unavailable
              ? `${bin.label}: not available`
              : binLabel(bin, series);
            if (bin.disabled)
              return (
                <button
                  key={bin.id}
                  type="button"
                  disabled
                  tabIndex={-1}
                  aria-label={name}
                  className={column}
                />
              );
            return (
              <TooltipTrigger
                key={bin.id}
                handle={tooltip}
                payload={name}
                type="button"
                ref={(node: HTMLElement | null) => {
                  if (node) buttons.current.set(bin.id, node);
                  else buttons.current.delete(bin.id);
                }}
                tabIndex={bin.id === stop ? 0 : -1}
                aria-label={name}
                aria-disabled={bin.unavailable ? true : undefined}
                aria-pressed={
                  onActivate && !bin.unavailable ? isSelected : undefined
                }
                data-selected={isSelected ? "true" : undefined}
                className={column}
                onFocus={() => {
                  if (!bin.unavailable) setActive(bin.id);
                }}
                onClick={() => {
                  if (!bin.unavailable) onActivate?.(bin.id);
                }}
              >
                {bin.unavailable ? (
                  <span
                    aria-hidden="true"
                    className="absolute inset-0 bg-[image:repeating-linear-gradient(135deg,currentColor_0_1px,transparent_1px_var(--space-1-5))] text-muted opacity-40"
                  />
                ) : (
                  <span
                    aria-hidden="true"
                    className="flex h-full w-4/5 max-w-6 items-end justify-center gap-px"
                  >
                    {series.map((entry, index) => {
                      const value = bin.values[index] ?? 0;
                      return (
                        <span
                          key={entry.name}
                          className={cn(
                            "max-w-3 min-w-px flex-1",
                            value > 0 && cn("min-h-0.5", binMark[entry.mark]),
                          )}
                          style={{ height: percent(value, top) }}
                        />
                      );
                    })}
                  </span>
                )}
              </TooltipTrigger>
            );
          })}
          {markerLeft !== undefined && marker && (
            <span
              className="pointer-events-none absolute inset-y-0 w-px bg-accent"
              style={{ left: markerLeft }}
            >
              <span className="sr-only">{marker.label}</span>
            </span>
          )}
        </div>
        <Tooltip handle={tooltip}>
          {({ payload }) =>
            typeof payload === "string" ? (
              <TooltipContent side="top">{payload}</TooltipContent>
            ) : null
          }
        </Tooltip>
      </div>
      <div
        aria-hidden="true"
        className="relative h-chart text-right font-mono text-xs text-muted tabular-nums"
      >
        <span className="invisible">{formatCount(top)}</span>
        {grid.map((value) => (
          <span
            key={value}
            className={cn(
              "absolute right-0 leading-ui",
              value === 0
                ? "bottom-0"
                : value === top
                  ? "top-0"
                  : "-translate-y-1/2",
            )}
            style={
              value === 0 || value === top
                ? undefined
                : { top: percent(top - value, top) }
            }
          >
            {formatCount(value)}
          </span>
        ))}
      </div>
      {ticks && (
        <div
          aria-hidden="true"
          className="flex min-w-0 font-mono text-xs text-muted"
        >
          {bins.map((bin) => (
            <span
              key={bin.id}
              className={cn(
                "flex min-w-0 flex-1 justify-center whitespace-nowrap",
                bin.id === selected && "font-medium text-fg",
              )}
            >
              {bin.tick}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

/** Series key built from the chart's own marks. */
export function BarChartLegend({
  series,
  className,
}: {
  series: BarChartSeries[];
  className?: string | undefined;
}) {
  return (
    <ul
      data-slot="bar-chart-legend"
      className={cn(
        "m-0 flex list-none flex-wrap items-center gap-3 p-0 font-mono text-xs text-muted",
        className,
      )}
    >
      {series.map((entry) => (
        <li key={entry.name} className="flex items-center gap-1">
          <span
            aria-hidden="true"
            className={cn("size-2 shrink-0", swatchMark[entry.mark])}
          />
          {entry.name}
        </li>
      ))}
    </ul>
  );
}
