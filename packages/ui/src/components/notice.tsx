import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps, ReactNode } from "react";
import { cn } from "@diffx/ui/lib/utils";

const noticeVariants = cva(
  "flex shrink-0 items-center gap-2 border-b border-border px-(--diff-gutter) py-1 text-sm [&_svg:not([class*='size-'])]:size-icon-sm",
  {
    variants: {
      tone: {
        accent: "bg-selected text-accent",
        warning: "bg-warning-bg text-warning",
        neutral: "bg-panel text-secondary",
      },
    },
    defaultVariants: { tone: "accent" },
  },
);

/**
 * A full-width status bar above the diff: an icon, one truncated line and an
 * optional action. Announced politely; stays mounted so updates are read.
 */
export function Notice({
  icon,
  action,
  tone,
  className,
  children,
  ...props
}: Omit<ComponentProps<"div">, "role"> &
  VariantProps<typeof noticeVariants> & {
    icon?: ReactNode;
    action?: ReactNode;
  }) {
  return (
    <div
      data-slot="notice"
      role="status"
      className={cn(noticeVariants({ tone }), className)}
      {...props}
    >
      {icon}
      <p className="m-0 min-w-0 flex-1 truncate">{children}</p>
      {action}
    </div>
  );
}
