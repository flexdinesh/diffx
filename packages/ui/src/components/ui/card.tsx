import type { ComponentProps } from "react";
import { cn } from "@diffx/ui/lib/utils";

// Review objects: a 28px neutral header, an 8px body and a 28px neutral action
// row. `inline` sits in Pierre annotations; `panel` is the sidebar card.
function Card({
  className,
  density = "inline",
  active = false,
  ...props
}: ComponentProps<"div"> & {
  density?: "inline" | "panel";
  /** Accent-tinted header for an editor in progress. */
  active?: boolean;
}) {
  return (
    <div
      data-slot="card"
      data-density={density}
      data-active={active || undefined}
      className={cn(
        "group/card flex min-w-0 flex-col overflow-hidden border border-border font-sans text-base leading-copy text-fg",
        density === "inline" ? "bg-raised" : "bg-bg",
        className,
      )}
      {...props}
    />
  );
}

function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "flex min-h-control-small items-center gap-1.5 border-b border-border bg-panel px-2 text-sm text-muted",
        "group-data-active/card:bg-selected group-data-[expanded=false]/card:border-b-0",
        className,
      )}
      {...props}
    />
  );
}

function CardTitle({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-title"
      className={cn("flex min-w-0 items-center gap-1", className)}
      {...props}
    />
  );
}

function CardAction({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn("ml-auto flex items-center self-center", className)}
      {...props}
    />
  );
}

function CardContent({ className, ...props }: ComponentProps<"div">) {
  return (
    <div data-slot="card-content" className={cn("p-2", className)} {...props} />
  );
}

function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "flex min-h-control-small items-center justify-end gap-1.5 border-t border-border bg-panel px-2 max-md:flex-wrap group-data-[density=panel]/card:flex-wrap",
        className,
      )}
      {...props}
    />
  );
}

export { Card, CardAction, CardContent, CardFooter, CardHeader, CardTitle };
