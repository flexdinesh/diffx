import { Toggle as TogglePrimitive } from "@base-ui/react/toggle";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const toggleVariants = cva(
  "group/toggle inline-flex items-center justify-center gap-1 whitespace-nowrap rounded-control text-sm font-medium text-secondary hover:bg-hover hover:text-fg disabled:pointer-events-none disabled:opacity-60 aria-invalid:border-error aria-pressed:bg-selected aria-pressed:text-accent [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-icon",
  {
    variants: {
      variant: {
        default: "bg-transparent",
        // Pairs with bordered fields such as the file filter.
        outline: "border border-separator bg-bg text-muted",
      },
      // Sizes match Button: xs 24, sm 28, default 32.
      size: {
        default: "h-control min-w-control px-2",
        sm: "h-control-small min-w-control-small px-2",
        xs: "h-control-compact min-w-control-compact px-2",
        "icon-sm": "size-control-small p-0",
      },
      touch: {
        true: "max-md:min-h-touch pointer-coarse:min-h-touch",
        false: "",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
      touch: false,
    },
  },
);

function Toggle({
  className,
  variant = "default",
  size = "default",
  touch = false,
  ...props
}: TogglePrimitive.Props & VariantProps<typeof toggleVariants>) {
  return (
    <TogglePrimitive
      data-slot="toggle"
      className={cn(toggleVariants({ variant, size, touch, className }))}
      {...props}
    />
  );
}

export { Toggle, toggleVariants };
