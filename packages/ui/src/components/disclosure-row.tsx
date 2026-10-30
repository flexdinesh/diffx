import type { ReactNode } from "react";
import { cn } from "@diffx/ui/lib/utils";
import { DisclosureIcon } from "./disclosure.tsx";
import { listRowVariants } from "./list-row.tsx";

/**
 * A group row with two targets: the leading chevron expands or collapses its
 * children, and the label performs the row's own action (e.g. opening a
 * repository's latest review). Both share one `list-row` surface.
 */
export function DisclosureRow({
  open,
  onToggle,
  toggleLabel,
  controls,
  onActivate,
  label,
  className,
  children,
}: {
  open: boolean;
  onToggle: () => void;
  /** Accessible name of the chevron, e.g. "Collapse diffx". */
  toggleLabel: string;
  /** ID of the children the chevron shows or hides. */
  controls: string;
  onActivate: () => void;
  /** Accessible name and tooltip of the label's action. */
  label: string;
  className?: string;
  /** The label's content: icon, name and trailing metadata. */
  children: ReactNode;
}) {
  return (
    <div className={cn(listRowVariants(), "gap-0 pr-0", className)}>
      <button
        type="button"
        className="-my-0.5 flex shrink-0 items-center self-stretch rounded-control px-1 text-muted hover:text-fg"
        aria-expanded={open}
        aria-controls={controls}
        aria-label={toggleLabel}
        onClick={onToggle}
      >
        <DisclosureIcon open={open} />
      </button>
      <button
        type="button"
        className="-my-0.5 flex min-w-0 flex-1 items-center gap-1.5 self-stretch rounded-control pr-1 pl-0.5 text-left"
        aria-label={label}
        title={label}
        onClick={onActivate}
      >
        {children}
      </button>
    </div>
  );
}
