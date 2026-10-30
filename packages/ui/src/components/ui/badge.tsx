import { mergeProps } from "@base-ui/react/merge-props";
import { useRender } from "@base-ui/react/use-render";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@diffx/ui/lib/utils";

const badgeVariants = cva(
  "group/badge inline-flex w-fit shrink-0 items-center justify-center gap-1 overflow-hidden whitespace-nowrap rounded-sm border text-xs [&>svg]:pointer-events-none [&>svg:not([class*='size-'])]:size-icon-sm",
  {
    variants: {
      variant: {
        // Numeric counts: tabs, filters, menus and file rows.
        count:
          "min-w-icon border-border bg-count px-1 font-mono leading-ui text-muted tabular-nums",
        // Labeled state with an optional icon: Open, Resolved, Outdated.
        status: "h-control-compact border-border bg-raised px-1.5 font-medium",
      },
      tone: {
        neutral: "",
        accent: "text-accent",
        success: "text-success",
        warning: "text-warning",
        error: "text-error",
      },
    },
    compoundVariants: [
      { variant: "status", tone: "neutral", className: "text-secondary" },
    ],
    defaultVariants: {
      variant: "count",
      tone: "neutral",
    },
  },
);

function Badge({
  className,
  variant = "count",
  tone = "neutral",
  render,
  ...props
}: useRender.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return useRender({
    defaultTagName: "span",
    props: mergeProps<"span">(
      {
        className: cn(badgeVariants({ variant, tone }), className),
      },
      props,
    ),
    render,
    state: {
      slot: "badge",
      variant,
    },
  });
}

export { Badge, badgeVariants };
