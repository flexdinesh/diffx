import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

// Decorative placeholder: hidden from assistive technology; pulses only when
// motion is allowed.
export function Skeleton({ className, ...props }: ComponentProps<"span">) {
  return (
    <span
      data-slot="skeleton"
      aria-hidden="true"
      className={cn(
        "skeleton block h-2 shrink-0 rounded-xs bg-separator motion-safe:animate-skeleton",
        className,
      )}
      {...props}
    />
  );
}
