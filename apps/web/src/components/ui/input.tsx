import * as React from "react";
import { Input as InputPrimitive } from "@base-ui/react/input";
import { cn } from "@/lib/utils";

// Touch-sized text prevents mobile browsers from zooming into the field.
const textEntryClassName =
  "w-full min-w-0 rounded-control border border-control bg-bg text-fg placeholder:text-muted disabled:pointer-events-none disabled:opacity-60 aria-invalid:border-error max-md:text-input-touch pointer-coarse:text-input-touch";

function Input({ className, type, ...props }: React.ComponentProps<"input">) {
  return (
    <InputPrimitive
      type={type}
      data-slot="input"
      className={cn(
        textEntryClassName,
        "h-control px-2 py-1 text-base",
        className,
      )}
      {...props}
    />
  );
}

export { Input, textEntryClassName };
