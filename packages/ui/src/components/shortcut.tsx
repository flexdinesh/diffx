import type { ComponentProps } from "react";
import { Kbd } from "@diffx/ui/components/ui/kbd";
import { formatShortcut, type Shortcut as Keys } from "../lib/format.ts";

/** A keyboard shortcut in the one platform format: ⌘K on macOS, Ctrl+K elsewhere. */
export function Shortcut({
  keys,
  ...props
}: Omit<ComponentProps<typeof Kbd>, "children"> & { keys: Keys }) {
  return <Kbd {...props}>{formatShortcut(keys)}</Kbd>;
}
