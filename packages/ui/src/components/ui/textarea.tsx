import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@diffx/ui/lib/utils";
import { textEntryClassName } from "@diffx/ui/components/ui/input";

const textareaVariants = cva("block resize-y", {
  variants: {
    variant: {
      // Review prose: comments and explanations.
      default: "min-h-24 px-3 py-2 text-md leading-copy",
      // Generated, selectable output such as exported review XML.
      code: "bg-panel p-3 font-mono text-sm leading-copy",
    },
  },
  defaultVariants: { variant: "default" },
});

function Textarea({
  className,
  variant = "default",
  ...props
}: React.ComponentProps<"textarea"> & VariantProps<typeof textareaVariants>) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(
        textEntryClassName,
        textareaVariants({ variant }),
        className,
      )}
      {...props}
    />
  );
}

export { Textarea };
