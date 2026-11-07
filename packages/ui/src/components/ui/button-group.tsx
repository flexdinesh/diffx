import type { ComponentProps } from "react";
import { cn } from "@diffx/ui/lib/utils";

// Joins adjacent controls into one boundary: inner corners square, shared
// edges collapse to a single border.
function ButtonGroup({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      role="group"
      data-slot="button-group"
      className={cn(
        "flex w-fit items-stretch *:focus-visible:relative *:focus-visible:z-10 [&>*:not(:first-child)]:border-l-0",
        className,
      )}
      {...props}
    />
  );
}

export { ButtonGroup };
