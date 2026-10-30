import { useEffect, useId, useState, type ReactNode } from "react";
import { Button } from "@diffx/ui/components/ui/button";
import { cn } from "@diffx/ui/lib/utils";
import { save, saved } from "../lib/storage.ts";
import { DisclosureIcon } from "./disclosure.tsx";

/** Open/closed state that persists per browser under `storageKey`. */
export function useSavedOpen(storageKey: string, defaultOpen = true) {
  const [open, setOpen] = useState(() => {
    const value = saved(storageKey);
    return value === null ? defaultOpen : value !== "true";
  });
  useEffect(() => {
    // The stored flag records "collapsed" for compatibility with saved state.
    save(storageKey, String(!open));
  }, [storageKey, open]);
  return [open, setOpen] as const;
}

/** A titled sidebar section whose body collapses under a full-width toggle. */
export function CollapsibleSection({
  title,
  label,
  storageKey,
  contentId,
  className,
  children,
}: {
  title: string;
  /** Accessible section name; defaults to the title. */
  label?: string;
  /** Persists the collapsed state per browser. */
  storageKey: string;
  contentId?: string;
  className?: string;
  children: ReactNode;
}) {
  const [open, setOpen] = useSavedOpen(storageKey);
  const generatedId = useId();
  const id = contentId ?? generatedId;
  return (
    <section
      className={cn("shrink-0 border-t border-border", className)}
      aria-label={label ?? title}
    >
      <Button
        variant="subtle"
        size="sm"
        className="w-full justify-between px-3"
        aria-controls={id}
        aria-expanded={open}
        aria-label={`${open ? "Hide" : "Show"} ${(label ?? title).toLowerCase()}`}
        onClick={() => setOpen((value) => !value)}
      >
        {title}
        <DisclosureIcon open={open} />
      </Button>
      <div id={id} hidden={!open}>
        {children}
      </div>
    </section>
  );
}
