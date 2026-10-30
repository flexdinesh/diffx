import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@diffx/ui/lib/utils";

// Shapes mirror the real geometry they stand in for: text lines, code lines,
// icons, and compact or standard controls.
const skeletonVariants = cva(
  "block shrink-0 bg-separator motion-safe:animate-skeleton",
  {
    variants: {
      shape: {
        line: "h-2 rounded-xs",
        code: "h-1.5 rounded-xs",
        label: "h-3 rounded-xs",
        icon: "size-icon-sm rounded-xs",
        control: "h-control-compact rounded-control",
        field: "h-control rounded-control",
      },
    },
    defaultVariants: { shape: "line" },
  },
);

// Decorative placeholder: hidden from assistive technology; pulses only when
// motion is allowed.
export function Skeleton({
  className,
  shape,
  ...props
}: ComponentProps<"span"> & VariantProps<typeof skeletonVariants>) {
  return (
    <span
      data-slot="skeleton"
      aria-hidden="true"
      className={cn(skeletonVariants({ shape }), className)}
      {...props}
    />
  );
}
