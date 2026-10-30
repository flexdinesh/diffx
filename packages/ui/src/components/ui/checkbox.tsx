import { Checkbox as CheckboxPrimitive } from "@base-ui/react/checkbox";
import { CheckIcon } from "lucide-react";
import { cn } from "@diffx/ui/lib/utils";

function Checkbox({ className, ...props }: CheckboxPrimitive.Root.Props) {
  return (
    <CheckboxPrimitive.Root
      data-slot="checkbox"
      className={cn(
        "peer inline-flex size-icon shrink-0 items-center justify-center rounded-sm border border-control bg-bg text-on-accent disabled:cursor-not-allowed disabled:opacity-60 aria-invalid:border-error data-checked:border-accent data-checked:bg-accent",
        className,
      )}
      {...props}
    >
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center [&>svg]:size-3 [&>svg]:[stroke-width:var(--icon-stroke-chrome)]"
      >
        <CheckIcon aria-hidden="true" />
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  );
}

export { Checkbox };
