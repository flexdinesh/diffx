import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@diffx/ui/lib/utils";

const statusDotVariants = cva("inline-block shrink-0 rounded-round", {
  variants: {
    tone: {
      current: "",
      accent: "text-accent",
      success: "text-success",
      warning: "text-warning",
      error: "text-error",
    },
    size: {
      sm: "size-1.5",
      md: "size-2",
    },
    fill: {
      solid: "bg-current",
      ring: "border border-current",
      half: "border border-current bg-[linear-gradient(90deg,currentColor_50%,transparent_50%)]",
    },
  },
  defaultVariants: { tone: "current", size: "sm", fill: "solid" },
});

/**
 * A small state mark. Never the only signal: pair it with text, or give it a
 * `label` so it is announced as an image.
 */
export function StatusDot({
  tone,
  size,
  fill,
  label,
  className,
  ...props
}: Omit<ComponentProps<"span">, "children"> &
  VariantProps<typeof statusDotVariants> & { label?: string }) {
  return (
    <span
      data-slot="status-dot"
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
      className={cn(statusDotVariants({ tone, size, fill }), className)}
      {...props}
    />
  );
}
