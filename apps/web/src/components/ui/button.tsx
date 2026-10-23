import { Button as ButtonPrimitive } from "@base-ui/react/button";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-control border border-transparent text-base font-medium select-none disabled:pointer-events-none disabled:opacity-60 aria-invalid:border-error [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-icon",
  {
    variants: {
      variant: {
        default:
          "bg-accent text-on-accent hover:bg-accent-hover active:bg-accent-hover",
        outline:
          "border-control bg-bg text-fg hover:bg-hover data-popup-open:bg-hover",
        "outline-muted":
          "border-separator bg-bg text-fg hover:bg-hover data-popup-open:bg-hover",
        // Muted outline toggle whose pressed state reads as done (Reviewed).
        "outline-check":
          "border-separator bg-bg text-muted hover:bg-hover aria-pressed:text-success",
        ghost:
          "text-secondary hover:bg-hover hover:text-fg data-popup-open:bg-hover",
        // Ghost actions on the shell canvas, where --hover is indistinguishable.
        shell:
          "text-secondary hover:bg-shell-hover hover:text-fg data-popup-open:bg-shell-hover [&_svg]:[stroke-width:var(--icon-stroke-chrome)]",
        // Review rail actions and repository marks on the panel; the current
        // repository keeps the accent fill.
        rail: "text-secondary hover:bg-hover hover:text-fg aria-[current=page]:bg-selected aria-[current=page]:text-accent aria-[current=true]:bg-selected aria-[current=true]:text-accent [&_svg]:[stroke-width:var(--icon-stroke-chrome)]",
        // Tertiary controls such as disclosure toggles and collapse arrows.
        subtle: "text-muted hover:bg-hover hover:text-fg",
        destructive: "text-error hover:bg-hover data-popup-open:bg-hover",
        "destructive-solid":
          "bg-error text-on-accent hover:bg-error-hover active:bg-error-hover",
        link: "text-accent underline-offset-(--space-1) hover:underline",
      },
      size: {
        default: "h-control gap-2 px-3",
        xs: "h-control-compact gap-1 px-2 text-sm",
        sm: "h-control-small gap-1 px-2 text-sm",
        lg: "h-control-large gap-2 px-3",
        // Text triggers inside a status row take the row's type and inset.
        inline: "h-control-compact gap-1 px-0 [font:inherit]",
        icon: "size-control p-0",
        "icon-xs": "size-control-compact p-0",
        "icon-sm": "size-control-small p-0",
        "icon-lg": "size-control-large p-0",
      },
      // App chrome reaches 44px targets on narrow or coarse-pointer screens.
      // Pierre header slots and drawer navigation keep desktop density.
      touch: {
        true: "max-md:min-h-touch pointer-coarse:min-h-touch",
        false: "",
      },
    },
    compoundVariants: [
      {
        touch: true,
        size: ["icon", "icon-xs", "icon-sm", "icon-lg"],
        className: "max-md:min-w-touch pointer-coarse:min-w-touch",
      },
    ],
    defaultVariants: {
      variant: "default",
      size: "default",
      touch: false,
    },
  },
);

function Button({
  className,
  variant = "default",
  size = "default",
  touch = false,
  ...props
}: ButtonPrimitive.Props & VariantProps<typeof buttonVariants>) {
  return (
    <ButtonPrimitive
      data-slot="button"
      data-size={size}
      data-variant={variant}
      className={cn(buttonVariants({ variant, size, touch, className }))}
      {...props}
    />
  );
}

export { Button, buttonVariants };
