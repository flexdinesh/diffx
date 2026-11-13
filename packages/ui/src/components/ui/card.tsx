import type { ComponentProps } from "react";
import { cn } from "@diffx/ui/lib/utils";

// Review objects: one flat framed surface with 8/12 padding, a 24px header row
// and an action row, separated by spacing rather than bars. `inline` sits in
// Pierre annotations; `panel` is the sidebar card.
function Card({
  className,
  density = "inline",
  ...props
}: ComponentProps<"div"> & {
  density?: "inline" | "panel";
}) {
  return (
    <div
      data-slot="card"
      data-density={density}
      className={cn(
        "group/card flex min-w-0 flex-col overflow-hidden border border-border bg-panel px-3 py-2 font-sans text-base leading-copy text-fg",
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
        "mb-1.5 flex min-h-control-compact items-center gap-2 text-sm text-muted",
        "group-data-[expanded=false]/card:mb-0",
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
  return <div data-slot="card-content" className={className} {...props} />;
}

function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "mt-2 flex items-center justify-end gap-1 max-md:flex-wrap group-data-[density=panel]/card:flex-wrap",
        className,
      )}
      {...props}
    />
  );
}

export { Card, CardAction, CardContent, CardFooter, CardHeader, CardTitle };
