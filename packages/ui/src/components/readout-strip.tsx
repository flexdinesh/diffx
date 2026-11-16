import { cn } from "@diffx/ui/lib/utils";

export interface ReadoutCell {
  value: string;
  label: string;
}

/**
 * Summary figures as one bordered strip: "47 merged | 12 open". Not tiles; the
 * value stays at body size. Below 768px the cells wrap to two columns. The
 * strip draws its top and left edges and each cell its right and bottom, so
 * no border doubles in either layout.
 */
export function ReadoutStrip({
  label,
  cells,
  className,
}: {
  /** Accessible list name, e.g. "Activity summary". */
  label: string;
  cells: ReadoutCell[];
  className?: string | undefined;
}) {
  return (
    <ul
      data-slot="readout-strip"
      aria-label={label}
      className={cn(
        "m-0 grid list-none grid-cols-2 border-t border-l border-border p-0 md:flex md:w-fit",
        className,
      )}
    >
      {cells.map((cell) => (
        <li
          key={cell.label}
          className="flex min-w-0 items-baseline gap-1 border-r border-b border-border px-3 py-1.5 text-sm md:grow"
        >
          <span className="font-mono font-medium text-fg tabular-nums">
            {cell.value}
          </span>
          {/* Flex drops this space visually; it keeps "47 merged" one phrase. */}{" "}
          <span className="min-w-0 truncate text-secondary">{cell.label}</span>
        </li>
      ))}
    </ul>
  );
}
