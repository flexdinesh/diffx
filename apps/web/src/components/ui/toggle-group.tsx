import * as React from "react";
import { Toggle as TogglePrimitive } from "@base-ui/react/toggle";
import { ToggleGroup as ToggleGroupPrimitive } from "@base-ui/react/toggle-group";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

import { toggleVariants } from "@/components/ui/toggle";

const toggleGroupVariants = cva(
  "group/toggle-group flex w-fit flex-row items-center rounded-control data-vertical:flex-col data-vertical:items-stretch",
  {
    variants: {
      variant: {
        default: "gap-2",
        // One bordered control; 1px optical inset around pressed items.
        segmented:
          "min-h-control-small gap-px border border-separator bg-panel p-px whitespace-nowrap",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

type ToggleGroupVariant = VariantProps<typeof toggleGroupVariants>["variant"];

const ToggleGroupContext = React.createContext<
  Pick<VariantProps<typeof toggleVariants>, "size" | "touch"> & {
    variant?: ToggleGroupVariant;
  }
>({ size: "default", variant: "default", touch: false });

function ToggleGroup({
  className,
  variant = "default",
  size,
  touch,
  orientation = "horizontal",
  children,
  ...props
}: ToggleGroupPrimitive.Props &
  VariantProps<typeof toggleGroupVariants> &
  Pick<VariantProps<typeof toggleVariants>, "size" | "touch"> & {
    orientation?: "horizontal" | "vertical";
  }) {
  return (
    <ToggleGroupPrimitive
      data-slot="toggle-group"
      data-variant={variant}
      data-size={size}
      data-orientation={orientation}
      className={cn(toggleGroupVariants({ variant }), className)}
      {...props}
    >
      <ToggleGroupContext.Provider value={{ variant, size, touch }}>
        {children}
      </ToggleGroupContext.Provider>
    </ToggleGroupPrimitive>
  );
}

function ToggleGroupItem({
  className,
  children,
  ...props
}: TogglePrimitive.Props) {
  const context = React.useContext(ToggleGroupContext);
  return (
    <TogglePrimitive
      data-slot="toggle-group-item"
      data-variant={context.variant}
      data-size={context.size}
      className={cn(
        toggleVariants({ size: context.size, touch: context.touch }),
        "shrink-0 focus:z-10 focus-visible:z-10",
        context.variant === "segmented" && "min-w-0 px-2",
        className,
      )}
      {...props}
    >
      {children}
    </TogglePrimitive>
  );
}

export { ToggleGroup, ToggleGroupItem };
