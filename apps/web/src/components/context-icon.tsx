import { FolderGit2Icon, SquareTerminalIcon } from "lucide-react";
import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

const contextIconVariants = cva("", {
  variants: {
    // Unset sizes follow the surrounding row's icon size.
    size: { inherit: "", md: "size-icon" },
    tone: { current: "", muted: "text-muted" },
  },
  defaultVariants: { size: "inherit", tone: "current" },
});

/**
 * A review context's kind: a repository, or a terminal for the piped capture
 * a capture process shows.
 */
export function ContextIcon({
  piped = false,
  size,
  tone,
  className,
  ...props
}: Omit<ComponentProps<typeof FolderGit2Icon>, "size"> &
  VariantProps<typeof contextIconVariants> & { piped?: boolean }) {
  const Icon = piped ? SquareTerminalIcon : FolderGit2Icon;
  return (
    <Icon
      aria-hidden="true"
      className={cn(contextIconVariants({ size, tone }), className)}
      {...props}
    />
  );
}
