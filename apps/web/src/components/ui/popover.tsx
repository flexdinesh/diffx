import * as React from "react";
import { Popover as PopoverPrimitive } from "@base-ui/react/popover";
import { cn } from "@/lib/utils";

function Popover(props: PopoverPrimitive.Root.Props) {
  return <PopoverPrimitive.Root data-slot="popover" {...props} />;
}

function PopoverTrigger(props: PopoverPrimitive.Trigger.Props) {
  return <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />;
}

function PopoverClose(props: PopoverPrimitive.Close.Props) {
  return <PopoverPrimitive.Close data-slot="popover-close" {...props} />;
}

// Anchored detail surfaces such as server status and diff comparison.
function PopoverContent({
  className,
  align = "end",
  alignOffset = 0,
  side = "top",
  sideOffset = 8,
  ...props
}: PopoverPrimitive.Popup.Props &
  Pick<
    PopoverPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset"
  >) {
  return (
    <PopoverPrimitive.Portal>
      <PopoverPrimitive.Positioner
        align={align}
        alignOffset={alignOffset}
        side={side}
        sideOffset={sideOffset}
        collisionPadding={16}
        className="isolate z-(--layer-popover)"
      >
        <PopoverPrimitive.Popup
          data-slot="popover-content"
          className={cn(
            "flex max-h-(--available-height) w-[min(16rem,calc(100vw-2*var(--space-4)))] flex-col gap-3 overflow-y-auto rounded-control bg-raised p-3 text-sm text-fg shadow-overlay ring-1 ring-border outline-none",
            className,
          )}
          {...props}
        />
      </PopoverPrimitive.Positioner>
    </PopoverPrimitive.Portal>
  );
}

function PopoverHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="popover-header"
      className={cn("flex flex-col gap-1", className)}
      {...props}
    />
  );
}

function PopoverTitle({ className, ...props }: PopoverPrimitive.Title.Props) {
  return (
    <PopoverPrimitive.Title
      data-slot="popover-title"
      className={cn(
        "m-0 flex items-center gap-2 text-sm font-semibold",
        className,
      )}
      {...props}
    />
  );
}

function PopoverDescription({
  className,
  ...props
}: PopoverPrimitive.Description.Props) {
  return (
    <PopoverPrimitive.Description
      data-slot="popover-description"
      className={cn("m-0 leading-copy text-secondary", className)}
      {...props}
    />
  );
}

/** A titled group of details below the popover header. */
function PopoverSection({
  className,
  title,
  children,
  ...props
}: React.ComponentProps<"section"> & { title: string }) {
  return (
    <section
      data-slot="popover-section"
      className={cn("border-t border-separator pt-3", className)}
      {...props}
    >
      <h2 className="m-0 mb-2 text-xs font-normal text-muted">{title}</h2>
      {children}
    </section>
  );
}

export {
  Popover,
  PopoverClose,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverSection,
  PopoverTitle,
  PopoverTrigger,
};
