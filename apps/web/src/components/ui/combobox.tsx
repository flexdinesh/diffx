import { Combobox as ComboboxPrimitive } from "@base-ui/react/combobox";
import { CheckIcon, SearchIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import {
  menuItemClassName,
  menuPopupClassName,
} from "@/components/ui/dropdown-menu";

// A menu whose rows filter as you type: a trigger opens a raised popup with
// the filter input on top, then the matching rows. Rows and surface match
// ui/dropdown-menu so switching menus read as one family.

const Combobox = ComboboxPrimitive.Root;

function ComboboxTrigger(props: ComboboxPrimitive.Trigger.Props) {
  return <ComboboxPrimitive.Trigger data-slot="combobox-trigger" {...props} />;
}

function ComboboxContent({
  align = "start",
  alignOffset = 0,
  side = "bottom",
  sideOffset = 4,
  className,
  ...props
}: ComboboxPrimitive.Popup.Props &
  Pick<
    ComboboxPrimitive.Positioner.Props,
    "align" | "alignOffset" | "side" | "sideOffset"
  >) {
  return (
    <ComboboxPrimitive.Portal>
      <ComboboxPrimitive.Positioner
        align={align}
        alignOffset={alignOffset}
        side={side}
        sideOffset={sideOffset}
        className="isolate z-(--layer-popover) outline-none"
      >
        <ComboboxPrimitive.Popup
          data-slot="combobox-content"
          className={cn(
            menuPopupClassName,
            "flex max-h-(--available-height) max-w-(--available-width) flex-col overflow-hidden",
            className,
          )}
          {...props}
        />
      </ComboboxPrimitive.Positioner>
    </ComboboxPrimitive.Portal>
  );
}

/** The filter field leading the popup; it owns focus while the popup is open. */
function ComboboxSearch({
  label,
  className,
  ...props
}: Omit<ComboboxPrimitive.Input.Props, "aria-label"> & {
  /** Accessible name; placeholders are hints, not labels. */
  label: string;
}) {
  return (
    <div className="shrink-0 p-1 pb-0">
      <InputGroup className={cn("border-separator", className)}>
        <InputGroupAddon>
          <SearchIcon aria-hidden="true" />
        </InputGroupAddon>
        <ComboboxPrimitive.Input
          data-slot="combobox-search"
          aria-label={label}
          render={<InputGroupInput />}
          {...props}
        />
      </InputGroup>
    </div>
  );
}

function ComboboxList({ className, ...props }: ComboboxPrimitive.List.Props) {
  return (
    <ComboboxPrimitive.List
      data-slot="combobox-list"
      className={cn(
        "min-h-0 flex-1 scroll-py-1 overflow-y-auto overscroll-contain p-1 data-empty:p-0",
        className,
      )}
      {...props}
    />
  );
}

/** A row; the selected row carries a trailing check. */
function ComboboxItem({
  className,
  children,
  ...props
}: ComboboxPrimitive.Item.Props) {
  return (
    <ComboboxPrimitive.Item
      data-slot="combobox-item"
      className={cn(menuItemClassName, className)}
      {...props}
    >
      {children}
      <ComboboxPrimitive.ItemIndicator
        data-slot="combobox-item-indicator"
        className="pointer-events-none absolute right-2 flex size-icon items-center justify-center"
      >
        <CheckIcon />
      </ComboboxPrimitive.ItemIndicator>
    </ComboboxPrimitive.Item>
  );
}

/** Shown in place of the list when the filter matches nothing. */
function ComboboxEmpty({ className, ...props }: ComboboxPrimitive.Empty.Props) {
  return (
    <ComboboxPrimitive.Empty
      data-slot="combobox-empty"
      className={cn(
        "px-2 py-3 text-center text-sm text-muted empty:hidden",
        className,
      )}
      {...props}
    />
  );
}

export {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxSearch,
  ComboboxTrigger,
};
