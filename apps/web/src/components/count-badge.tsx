import type { ComponentProps } from "react";
import { Badge } from "@/components/ui/badge";
import { formatCount } from "../format.ts";

/**
 * A numeric count chip. Pass `label` when the number alone does not say what
 * it counts, e.g. "3 changed files".
 */
export function CountBadge({
  count,
  label,
  ...props
}: Omit<ComponentProps<typeof Badge>, "variant" | "children"> & {
  count: number;
  label?: string;
}) {
  return (
    <Badge variant="count" aria-label={label} {...props}>
      {formatCount(count)}
    </Badge>
  );
}
