import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * The compact header row shared by navigation panels: 36px on desktop, the
 * topbar height inside compact drawers, a bottom separator, trailing actions.
 */
export function PanelHeader({
  title,
  actions,
  className,
  children,
  ...props
}: ComponentProps<"div"> & {
  /** Rendered as the panel's h2. Omit when children supply the label (tabs). */
  title?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <div
      data-slot="panel-header"
      className={cn(
        "flex h-panel-header min-w-0 shrink-0 items-center gap-0.5 border-b border-border px-1 max-lg:h-topbar [&_svg]:[stroke-width:var(--icon-stroke-chrome)]",
        title !== undefined && "gap-2 pl-4",
        className,
      )}
      {...props}
    >
      {title !== undefined && (
        <h2 className="m-0 min-w-0 flex-1 truncate text-sm leading-ui font-medium tracking-[inherit] text-secondary">
          {title}
        </h2>
      )}
      {children}
      {actions}
    </div>
  );
}
