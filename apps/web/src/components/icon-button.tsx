import type { ComponentProps, ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Kbd } from "@/components/ui/kbd";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { formatShortcut, type Shortcut } from "../format.ts";

type IconButtonProps = Omit<ComponentProps<typeof Button>, "aria-label"> & {
  /** Accessible name and tooltip text. */
  label: string;
  shortcut?: Shortcut;
  /** Overrides the tooltip text, e.g. transient "Copied" feedback. */
  hint?: string;
  children: ReactNode;
};

/**
 * An icon-only action. The label is its accessible name and its tooltip, so
 * the two never drift; a shortcut is shown in the tooltip, not the name.
 */
export function IconButton({
  label,
  shortcut,
  hint,
  variant = "ghost",
  size = "icon",
  children,
  ...props
}: IconButtonProps) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            type="button"
            variant={variant}
            size={size}
            aria-label={label}
            {...props}
          />
        }
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>
        {hint ?? label}
        {shortcut && <Kbd>{formatShortcut(shortcut)}</Kbd>}
      </TooltipContent>
    </Tooltip>
  );
}
