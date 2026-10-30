import { mergeProps } from "@base-ui/react/merge-props";
import { useRender } from "@base-ui/react/use-render";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@diffx/ui/lib/utils";

const listRowVariants = cva(
  "flex w-full min-w-0 min-h-control-small items-center gap-1.5 rounded-control px-1 py-0.5 text-left text-sm text-fg no-underline hover:bg-hover aria-[current=page]:bg-selected aria-[current=page]:text-accent aria-[current=true]:bg-selected aria-[current=true]:text-accent aria-selected:bg-selected aria-checked:bg-selected aria-checked:text-accent disabled:cursor-not-allowed disabled:text-secondary disabled:hover:bg-transparent [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-icon-sm",
  {
    variants: {
      // Paths and code identifiers stay mono; names of repos and branches sans.
      font: {
        sans: "leading-ui",
        mono: "font-mono leading-tree whitespace-pre",
      },
      variant: {
        default: "",
        // Two-line dialog results (review picker): roomier insets and muted
        // leading/trailing icons; touch-height rows on coarse pointers.
        option:
          "gap-2 rounded-md px-3 py-1.5 text-base max-md:px-2 pointer-coarse:min-h-touch [&>svg]:text-muted",
      },
    },
    defaultVariants: { font: "sans", variant: "default" },
  },
);

/**
 * The one navigable row: file tree entries, repos and streams, picker and menu
 * results, choice-list options. Current (`aria-current`), selected
 * (`aria-selected`) and checked (`aria-checked`) rows share the accent fill. `depth` indents nested tree rows.
 */
function ListRow({
  font,
  variant,
  depth,
  className,
  style,
  render,
  ...props
}: useRender.ComponentProps<"button"> &
  VariantProps<typeof listRowVariants> & { depth?: number }) {
  return useRender({
    defaultTagName: "button",
    props: mergeProps<"button">(
      {
        className: cn(listRowVariants({ font, variant }), className),
        style:
          depth === undefined
            ? style
            : {
                paddingLeft: `calc(var(--space-1) + ${depth} * var(--space-3))`,
                ...style,
              },
      },
      props,
    ),
    render,
    state: { slot: "list-row" },
  });
}

export { ListRow, listRowVariants };
