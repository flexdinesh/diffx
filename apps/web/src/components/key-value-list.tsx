import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";

/** Label/value rows: summary totals, server metrics, diff comparison details. */
export function KeyValueList({ className, ...props }: ComponentProps<"dl">) {
  return (
    <dl
      data-slot="key-value-list"
      className={cn("m-0 flex flex-col gap-1 text-sm", className)}
      {...props}
    />
  );
}

export function KeyValue({
  label,
  className,
  children,
  ...props
}: ComponentProps<"dd"> & { label: ReactNode }) {
  return (
    <div className="flex min-w-0 items-center justify-between gap-2">
      <dt className="min-w-0 text-secondary">{label}</dt>
      <dd
        className={cn(
          "m-0 min-w-0 text-right font-medium text-fg tabular-nums",
          className,
        )}
        {...props}
      >
        {children}
      </dd>
    </div>
  );
}
