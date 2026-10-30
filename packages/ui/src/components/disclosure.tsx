import { ChevronRightIcon } from "lucide-react";
import { cn } from "@diffx/ui/lib/utils";

/**
 * The one expand/collapse affordance: points right when collapsed and down
 * when expanded. Purely visual; the owning control carries aria-expanded.
 */
export function DisclosureIcon({
  open,
  className,
}: {
  open: boolean;
  className?: string;
}) {
  return (
    <ChevronRightIcon
      aria-hidden="true"
      data-open={open || undefined}
      className={cn(
        "size-icon-sm shrink-0 motion-safe:transition-transform motion-safe:duration-(--duration-fast) data-open:rotate-90",
        className,
      )}
    />
  );
}
