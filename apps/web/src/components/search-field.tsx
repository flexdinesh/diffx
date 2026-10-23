import { SearchIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Kbd } from "@/components/ui/kbd";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { formatShortcut, type Shortcut } from "../format.ts";

/** The one search/filter field: icon, input, optional shortcut and actions. */
export function SearchField({
  label,
  shortcut,
  actions,
  className,
  ...input
}: Omit<ComponentProps<"input">, "aria-label"> & {
  /** Accessible name; placeholders are hints, not labels. */
  label: string;
  shortcut?: Shortcut;
  /** Trailing icon buttons, e.g. filters or close. */
  actions?: ReactNode;
}) {
  return (
    <InputGroup className={className}>
      <InputGroupAddon>
        <SearchIcon aria-hidden="true" />
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
