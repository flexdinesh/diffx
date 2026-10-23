import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

/** A failed load or action with the one retry affordance, labeled "Retry". */
export function ErrorState({
  message,
  onRetry,
  busy = false,
  className,
  ...props
}: Omit<ComponentProps<"div">, "role"> & {
  message: string;
  onRetry?: () => void;
  busy?: boolean;
}) {
  return (
    <div
      data-slot="error-state"
      role="alert"
      className={cn(
        "flex flex-wrap items-center gap-2 text-sm leading-copy text-error",
        className,
      )}
      {...props}
    >
      <p className="m-0 min-w-0">{message}</p>
      {onRetry && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          aria-busy={busy}
          onClick={onRetry}
        >
          Retry
        </Button>
      )}
    </div>
  );
}
