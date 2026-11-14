import { Tabs as TabsPrimitive } from "@base-ui/react/tabs";
import { cn } from "@diffx/ui/lib/utils";

function Tabs({
  className,
  orientation = "horizontal",
  ...props
}: TabsPrimitive.Root.Props) {
  return (
    <TabsPrimitive.Root
      data-slot="tabs"
      data-orientation={orientation}
      className={cn("group/tabs flex flex-col gap-2", className)}
      {...props}
    />
  );
}

// Panel-header tabs: equal-width labels flush with the header's leading edge.
// The active tab fuses with the panel below it and, when last, with the actions
// beside it; inactive tabs recess into the shell and draw their own edges. The
// only tab style in the app.
function TabsList({ className, ...props }: TabsPrimitive.List.Props) {
  return (
    <TabsPrimitive.List
      data-slot="tabs-list"
      className={cn(
        "relative -ml-1 flex min-w-0 flex-1 items-stretch self-stretch",
        className,
      )}
      {...props}
    />
  );
}

function TabsTrigger({ className, ...props }: TabsPrimitive.Tab.Props) {
  return (
    <TabsPrimitive.Tab
      data-slot="tabs-trigger"
      className={cn(
        "relative -mb-px flex min-w-0 flex-1 items-center justify-center gap-1 border-r border-border bg-tab-inactive px-1 text-sm font-medium whitespace-nowrap text-secondary hover:text-fg disabled:pointer-events-none disabled:opacity-60 data-active:bg-panel data-active:text-fg [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-icon",
        "not-data-active:border-b",
        // The selected tab's count shares the selected-row treatment.
        "data-active:**:data-[slot=badge]:border-accent/35 data-active:**:data-[slot=badge]:bg-selected data-active:**:data-[slot=badge]:text-accent",
        className,
      )}
      {...props}
    />
  );
}

function TabsContent({ className, ...props }: TabsPrimitive.Panel.Props) {
  return (
    <TabsPrimitive.Panel
      data-slot="tabs-content"
      className={cn("flex-1 text-sm outline-none", className)}
      {...props}
    />
  );
}

export { Tabs, TabsList, TabsTrigger, TabsContent };
