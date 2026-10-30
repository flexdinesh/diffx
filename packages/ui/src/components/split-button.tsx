import { ChevronDownIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Button } from "@diffx/ui/components/ui/button";
import { ButtonGroup } from "@diffx/ui/components/ui/button-group";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { cn } from "@diffx/ui/lib/utils";
import { IconButton } from "./icon-button.tsx";

type ButtonProps = ComponentProps<typeof Button>;

/** One primary action joined to a menu of broader or narrower variants. */
export function SplitButton({
  variant = "outline",
  size = "sm",
  touch,
  menuLabel,
  menu,
  menuAlign = "end",
  className,
  ...primary
}: Omit<ButtonProps, "size"> & {
  size?: "xs" | "sm" | "default";
  /** Names the menu trigger, e.g. "Copy options". */
  menuLabel: string;
  menu: ReactNode;
  menuAlign?: "start" | "end";
}) {
  const triggerSize =
    size === "xs" ? "icon-xs" : size === "sm" ? "icon-sm" : "icon";
  return (
    <ButtonGroup className={cn("min-w-0", className)}>
      <Button
        type="button"
        variant={variant}
        size={size}
        touch={touch}
        className="min-w-0 flex-1"
        {...primary}
      />
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <IconButton
              label={menuLabel}
              variant={variant}
              size={triggerSize}
              touch={touch}
            >
              <ChevronDownIcon aria-hidden="true" />
            </IconButton>
          }
        />
        <DropdownMenuContent align={menuAlign} aria-label={menuLabel}>
          {menu}
        </DropdownMenuContent>
      </DropdownMenu>
    </ButtonGroup>
  );
}
