import { FolderGit2Icon, SquareTerminalIcon } from "lucide-react";
import type { ComponentProps } from "react";

/** A review context's kind: a terminal for piped input, a repository otherwise. */
export function ContextIcon({
  piped,
  ...props
}: ComponentProps<typeof FolderGit2Icon> & { piped: boolean }) {
  const Icon = piped ? SquareTerminalIcon : FolderGit2Icon;
  return <Icon aria-hidden="true" {...props} />;
}
