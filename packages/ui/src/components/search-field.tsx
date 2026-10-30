import { SearchIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Kbd } from "@diffx/ui/components/ui/kbd";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@diffx/ui/components/ui/input-group";
import { cn } from "@diffx/ui/lib/utils";
import { formatShortcut, type Shortcut } from "../lib/format.ts";

/** The one search/filter field: icon, input, optional shortcut and actions. */
export function SearchField({
  label,
  shortcut,
  leading,
  actions,
  className,
  ...input
}: Omit<ComponentProps<"input">, "aria-label"> & {
  /** Accessible name; placeholders are hints, not labels. */
  label: string;
  shortcut?: Shortcut;
  /** Replaces the search icon, e.g. a back action in a drilled-in list. */
  leading?: ReactNode;
  /** Trailing icon buttons, e.g. filters or close. */
  actions?: ReactNode;
}) {
  return (
    // Search shares the quiet --border-muted boundary of the toolbar controls
    // beside it; forms keep the stronger control boundary.
    <InputGroup className={cn("border-separator", className)}>
      <InputGroupAddon>
        {leading ?? <SearchIcon aria-hidden="true" />}
      </InputGroupAddon>
      <InputGroupInput type="search" aria-label={label} {...input} />
      {(shortcut || actions) && (
        <InputGroupAddon align="end" className="-mr-1.5">
          {shortcut && (
            <Kbd className="max-md:hidden">{formatShortcut(shortcut)}</Kbd>
          )}
          {actions}
        </InputGroupAddon>
      )}
    </InputGroup>
  );
}
