"use client";

import { Separator as SeparatorPrimitive } from "@base-ui/react/separator";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const separatorVariants = cva(
  "shrink-0 data-horizontal:h-px data-horizontal:w-full data-vertical:w-px",
  {
    variants: {
      variant: {
        default: "bg-separator data-vertical:self-stretch",
        // Header chrome: a short, centered rule between brand and context.
        chrome: "bg-border data-vertical:h-4 data-vertical:self-center",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

function Separator({
  className,
  orientation = "horizontal",
  variant,
  ...props
}: SeparatorPrimitive.Props & VariantProps<typeof separatorVariants>) {
  return (
    <SeparatorPrimitive
      data-slot="separator"
      orientation={orientation}
      className={cn(separatorVariants({ variant }), className)}
      {...props}
    />
  );
}

export { Separator };
