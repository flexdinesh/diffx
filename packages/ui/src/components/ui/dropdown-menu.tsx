import { Menu as MenuPrimitive } from "@base-ui/react/menu";
import { CheckIcon } from "lucide-react";
import type { ComponentProps } from "react";
import { cn } from "@diffx/ui/lib/utils";

/** Menu and combobox popups share one raised surface. */
export const menuPopupClassName =
  "min-w-32 rounded-control bg-raised text-fg shadow-overlay ring-1 ring-border outline-none";

/** Menu and combobox rows share one highlighted/disabled look. */
export const menuItemClassName =
  "relative flex min-h-control cursor-default items-center gap-2 rounded-control py-1 pr-8 pl-2 text-sm outline-hidden select-none focus:bg-selected focus:text-accent data-highlighted:bg-selected data-highlighted:text-accent data-disabled:pointer-events-none data-disabled:opacity-60 [&_svg]:pointer-events-none [&_svg]:size-icon [&_svg]:shrink-0";

function DropdownMenu(props: MenuPrimitive.Root.Props) {
  return <MenuPrimitive.Root data-slot="dropdown-menu" {...props} />;
}

function DropdownMenuTrigger(props: MenuPrimitive.Trigger.Props) {
  return <MenuPrimitive.Trigger data-slot="dropdown-menu-trigger" {...props} />;
}

function DropdownMenuContent({
  align = "start",
  alignOffset = 0,
  side = "bottom",
  sideOffset = 4,
  className,
  ...props
}: MenuPrimitive.Popup.Props &
  Pick<
    MenuPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset"
  >) {
  return (
    <MenuPrimitive.Portal>
      <MenuPrimitive.Positioner
        align={align}
        alignOffset={alignOffset}
        side={side}
        sideOffset={sideOffset}
        className="isolate z-(--layer-popover) outline-none"
      >
        <MenuPrimitive.Popup
          data-slot="dropdown-menu-content"
          className={cn(
            menuPopupClassName,
            "max-h-(--available-height) overflow-x-hidden overflow-y-auto p-1",
            className,
          )}
          {...props}
        />
      </MenuPrimitive.Positioner>
    </MenuPrimitive.Portal>
  );
}

function DropdownMenuGroup(props: MenuPrimitive.Group.Props) {
  return <MenuPrimitive.Group data-slot="dropdown-menu-group" {...props} />;
}

function DropdownMenuLabel({
  className,
  ...props
}: MenuPrimitive.GroupLabel.Props) {
  return (
    <MenuPrimitive.GroupLabel
      data-slot="dropdown-menu-label"
      className={cn(
        "px-2 pt-2 pb-1 text-xs font-semibold text-secondary",
        className,
      )}
      {...props}
    />
  );
}

/** A non-interactive heading for a menu without groups. */
function DropdownMenuHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="dropdown-menu-header"
      className={cn(
        "px-2 pt-1 pb-2 text-sm font-medium text-secondary",
        className,
      )}
      {...props}
    />
  );
}

function DropdownMenuSeparator({
  className,
  ...props
}: MenuPrimitive.Separator.Props) {
  return (
    <MenuPrimitive.Separator
      data-slot="dropdown-menu-separator"
      className={cn("-mx-1 my-1 h-px bg-separator", className)}
      {...props}
    />
  );
}

function DropdownMenuRadioGroup(props: MenuPrimitive.RadioGroup.Props) {
  return (
    <MenuPrimitive.RadioGroup
      data-slot="dropdown-menu-radio-group"
      {...props}
    />
  );
}

function DropdownMenuItem({ className, ...props }: MenuPrimitive.Item.Props) {
  return (
    <MenuPrimitive.Item
      data-slot="dropdown-menu-item"
      className={cn(menuItemClassName, className)}
      {...props}
    />
  );
}

function DropdownMenuRadioItem({
  className,
  children,
  indicator = "end",
  ...props
}: MenuPrimitive.RadioItem.Props & {
  /** `start` leads with the check, leaving the trailing edge for counts. */
  indicator?: "start" | "end";
}) {
  return (
    <MenuPrimitive.RadioItem
      data-slot="dropdown-menu-radio-item"
      className={cn(
        menuItemClassName,
        indicator === "start" && "pr-1 pl-8",
        className,
      )}
      {...props}
    >
      {children}
      <MenuPrimitive.RadioItemIndicator
        data-slot="dropdown-menu-radio-item-indicator"
        className={cn(
          "pointer-events-none absolute flex size-icon items-center justify-center",
          indicator === "start" ? "left-2" : "right-2",
        )}
      >
        <CheckIcon />
      </MenuPrimitive.RadioItemIndicator>
    </MenuPrimitive.RadioItem>
  );
}

function DropdownMenuCheckboxItem({
  className,
  children,
  ...props
}: MenuPrimitive.CheckboxItem.Props) {
  return (
    <MenuPrimitive.CheckboxItem
      data-slot="dropdown-menu-checkbox-item"
      className={cn(menuItemClassName, className)}
      {...props}
    >
      {children}
      <MenuPrimitive.CheckboxItemIndicator className="pointer-events-none absolute right-2 flex size-icon items-center justify-center">
        <CheckIcon />
      </MenuPrimitive.CheckboxItemIndicator>
    </MenuPrimitive.CheckboxItem>
  );
}

export {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuHeader,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
};
