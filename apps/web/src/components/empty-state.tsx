import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/lib/utils";

/**
 * What a surface shows when it has nothing to list. `page` centers a heading
 * and copy in a main region; `section` sits inside a panel or menu.
 */
export function EmptyState({
  title,
  heading = "h2",
  description,
  action,
  size = "section",
  align = size === "page" ? "center" : "start",
  className,
  ...props
}: Omit<ComponentProps<"div">, "title"> & {
  title?: ReactNode;
  /** Page titles render as the region's h1. */
  heading?: "h1" | "h2" | "p";
  description?: ReactNode;
  action?: ReactNode;
  size?: "page" | "section";
  align?: "start" | "center";
}) {
  const Title = heading;
  return (
    <div
      data-slot="empty-state"
      className={cn(
        "flex flex-col gap-1",
        align === "center" ? "items-center text-center" : "items-start",
        size === "page" ? "gap-2 p-6" : "px-2 py-2",
        className,
      )}
      {...props}
    >
      {title !== undefined && (
        <Title
          className={cn(
            "m-0",
            size === "page"
              ? "text-xl leading-ui font-semibold tracking-heading text-fg"
              : "text-sm leading-ui font-medium text-secondary",
          )}
        >
          {title}
        </Title>
      )}
      {description !== undefined && (
        <p
          className={cn(
            "m-0 max-w-(--reading-width) leading-copy",
            size === "page" ? "text-md text-secondary" : "text-sm text-muted",
          )}
        >
          {description}
        </p>
      )}
      {action !== undefined && (
        <div className={cn("flex gap-2", size === "page" && "mt-2")}>
          {action}
        </div>
      )}
    </div>
  );
}
