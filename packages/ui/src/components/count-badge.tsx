import type { ComponentProps } from "react";
import { Badge } from "@diffx/ui/components/ui/badge";
import { formatCount, pluralize } from "../lib/format.ts";

/**
 * A numeric count chip. Pass `label` when the number alone does not say what
 * it counts, e.g. "3 changed files", or `unit` to show it: "3 changed files".
 */
export function CountBadge({
  count,
  label,
  unit,
  ...props
}: Omit<ComponentProps<typeof Badge>, "variant" | "children"> & {
  count: number;
  label?: string;
  /** Singular noun shown after the count, pluralized by `pluralize`. */
  unit?: string;
}) {
  return (
    <Badge variant="count" aria-label={label} {...props}>
      {unit ? pluralize(count, unit) : formatCount(count)}
    </Badge>
  );
}
