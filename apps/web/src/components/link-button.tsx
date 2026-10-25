import type { VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/**
 * Navigation that looks like a Button but stays a link, so assistive
 * technology and the browser treat it as one. External targets open in a new
 * tab without an opener.
 */
export function LinkButton({
  variant = "outline",
  size = "default",
  touch = false,
  external = false,
  className,
  ...props
}: ComponentProps<"a"> &
  VariantProps<typeof buttonVariants> & { external?: boolean }) {
  return (
    <a
      data-slot="link-button"
      className={cn(
        buttonVariants({ variant, size, touch }),
        "no-underline",
        className,
      )}
      {...(external && { target: "_blank", rel: "noopener noreferrer" })}
      {...props}
    />
  );
}
