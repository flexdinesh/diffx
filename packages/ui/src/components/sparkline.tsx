import { cn } from "@diffx/ui/lib/utils";

/**
 * A tiny bar trend beside a row's figures. Decorative unless `label` names
 * it; zero bins keep a 1px baseline so gaps read as "none", not missing.
 */
export function Sparkline({
  values,
  max,
  label,
  className,
}: {
  values: number[];
  /** Shared scale across sibling sparklines; defaults to the largest value. */
  max?: number | undefined;
  label?: string | undefined;
  className?: string | undefined;
}) {
  const top = Math.max(max ?? 0, ...values, 1);
  return (
    <span
      data-slot="sparkline"
      {...(label
        ? { role: "img", "aria-label": label }
        : { "aria-hidden": true })}
      className={cn("flex h-4 w-full min-w-0 items-end gap-px", className)}
    >
      {values.map((value, index) => (
        <span
          // Positions are the identity: bins never reorder.
          key={index}
          className={cn(
            "min-w-px flex-1",
            value > 0 ? "min-h-px bg-current text-muted" : "h-px bg-border",
          )}
          style={
            value > 0
              ? { height: `${(Math.min(value, top) / top) * 100}%` }
              : undefined
          }
        />
      ))}
    </span>
  );
}
