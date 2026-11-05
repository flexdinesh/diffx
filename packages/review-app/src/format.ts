import { formatCount } from "@diffx/ui/lib/format";

/** Reviewed progress, identical in the summary and the status bar. */
export function formatProgress(done: number, total: number): string {
  return `${formatCount(done)} of ${formatCount(total)} reviewed`;
}
