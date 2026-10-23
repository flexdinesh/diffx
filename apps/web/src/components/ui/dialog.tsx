"use client";

import * as React from "react";
import { Dialog as DialogPrimitive } from "@base-ui/react/dialog";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

function Dialog({ ...props }: DialogPrimitive.Root.Props) {
  return <DialogPrimitive.Root data-slot="dialog" {...props} />;
}

function DialogTrigger({ ...props }: DialogPrimitive.Trigger.Props) {
  return <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />;
}

function DialogPortal({ ...props }: DialogPrimitive.Portal.Props) {
  return <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />;
}

function DialogClose({ ...props }: DialogPrimitive.Close.Props) {
  return <DialogPrimitive.Close data-slot="dialog-close" {...props} />;
}

function DialogOverlay({
  className,
  ...props
}: DialogPrimitive.Backdrop.Props) {
  return (
    <DialogPrimitive.Backdrop
      data-slot="dialog-overlay"
      className={cn(
        "fixed inset-0 isolate z-(--layer-popover) bg-backdrop",
        className,
      )}
      {...props}
    />
  );
}

// Every dialog keeps a visible title and explicit actions; no corner close.
const dialogContentVariants = cva(
  "fixed top-1/2 left-1/2 z-(--layer-popover) grid max-h-[calc(100dvh-2*var(--page-gutter))] w-[min(var(--reading-width),calc(100%-2*var(--page-gutter)))] -translate-x-1/2 -translate-y-1/2 gap-4 overflow-auto rounded-lg border border-border bg-raised p-6 text-md text-fg shadow-overlay outline-none",
  {
    variants: {
      variant: {
        default: "",
        // A command palette anchored below the header: search, list, footer.
        command:
          "top-[calc(var(--topbar-height)+var(--space-6))] max-h-[calc(100dvh-var(--topbar-height)-var(--space-6)-var(--page-gutter))] w-[min(35rem,calc(100%-2*var(--page-gutter)))] translate-y-0 grid-rows-[auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 max-md:top-4 max-md:max-h-[calc(100dvh-var(--space-4)-var(--page-gutter))]",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

function DialogContent({
  className,
  variant,
  children,
  ...props
}: DialogPrimitive.Popup.Props & VariantProps<typeof dialogContentVariants>) {
  return (
    <DialogPortal>
      <DialogOverlay />
      <DialogPrimitive.Popup
        data-slot="dialog-content"
        className={cn(dialogContentVariants({ variant }), className)}
        {...props}
      >
        {children}
      </DialogPrimitive.Popup>
    </DialogPortal>
  );
}

function DialogHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-header"
      className={cn("flex flex-col gap-2", className)}
      {...props}
    />
  );
}

function DialogFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="dialog-footer"
      className={cn(
        "flex flex-col-reverse gap-2 pt-3 md:flex-row md:justify-end",
        className,
      )}
      {...props}
    />
  );
}

function DialogTitle({ className, ...props }: DialogPrimitive.Title.Props) {
  return (
    <DialogPrimitive.Title
      data-slot="dialog-title"
      className={cn(
        "m-0 text-xl leading-ui font-semibold tracking-heading",
        className,
      )}
      {...props}
    />
  );
}

function DialogDescription({
  className,
  ...props
}: DialogPrimitive.Description.Props) {
  return (
    <DialogPrimitive.Description
      data-slot="dialog-description"
      className={cn(
        "m-0 text-md leading-copy text-secondary *:[a]:underline *:[a]:underline-offset-(--space-1) *:[a]:hover:text-fg",
        className,
      )}
      {...props}
    />
  );
}

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
};
