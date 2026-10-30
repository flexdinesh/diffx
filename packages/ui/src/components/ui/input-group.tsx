import * as React from "react";
import { cn } from "@diffx/ui/lib/utils";
import { Input } from "@diffx/ui/components/ui/input";

// One bordered field holding an input plus icons, hints or actions. Focus
// rings the whole group, not the inner input.
function InputGroup({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="input-group"
      role="group"
      className={cn(
        "group/input-group relative flex h-control w-full min-w-0 items-center gap-1.5 rounded-control border border-control bg-bg px-2 text-muted has-[input:focus-visible]:[outline:var(--focus-ring)] has-[input:focus-visible]:[outline-offset:var(--focus-offset)] has-[[aria-invalid=true]]:border-error",
        className,
      )}
      {...props}
    />
  );
}

function InputGroupAddon({
  className,
  align = "start",
  onClick,
  ...props
}: React.ComponentProps<"div"> & { align?: "start" | "end" }) {
  return (
    <div
      data-slot="input-group-addon"
      data-align={align}
      className={cn(
        "flex shrink-0 cursor-text items-center gap-1 select-none [&>svg:not([class*='size-'])]:size-icon",
        align === "start" ? "order-first" : "order-last",
        className,
      )}
      onClick={(event) => {
        onClick?.(event);
        if (
          event.target instanceof Element &&
          event.target.closest("button, a")
        )
          return;
        event.currentTarget.parentElement?.querySelector("input")?.focus();
      }}
      {...props}
    />
  );
}

function InputGroupInput({
  className,
  ...props
}: React.ComponentProps<"input">) {
  return (
    <Input
      data-slot="input-group-control"
      className={cn(
        "h-full flex-1 rounded-none border-0 bg-transparent px-0 py-0 outline-none focus-visible:outline-none",
        className,
      )}
      {...props}
    />
  );
}

export { InputGroup, InputGroupAddon, InputGroupInput };
