/** Shared UI copy formatting: one style for counts, plurals, progress and shortcuts. */

export function formatCount(count: number): string {
  return count.toLocaleString();
}

/** `pluralize(1, "file")` → "1 file"; `pluralize(2, "comment")` → "2 comments". */
export function pluralize(
  count: number,
  singular: string,
  plural = `${singular}s`,
): string {
  return `${formatCount(count)} ${count === 1 ? singular : plural}`;
}

/** Reviewed progress, identical in the summary and the status bar. */
export function formatProgress(done: number, total: number): string {
  return `${formatCount(done)} of ${formatCount(total)} reviewed`;
}

export interface Shortcut {
  /** ⌘ on macOS, Ctrl elsewhere. */
  mod?: boolean;
  alt?: boolean;
  shift?: boolean;
  key: string;
}

export function isMac(
  platform = globalThis.navigator?.platform ?? "",
): boolean {
  return /Mac|iPhone|iPad/.test(platform);
}

/** macOS uses joined symbols (⌘K, ⌥R); other platforms use Ctrl+K, Alt+R. */
export function formatShortcut(shortcut: Shortcut, mac = isMac()): string {
  const keys = [
    shortcut.mod ? (mac ? "⌘" : "Ctrl") : "",
    shortcut.alt ? (mac ? "⌥" : "Alt") : "",
    shortcut.shift ? (mac ? "⇧" : "Shift") : "",
    shortcut.key,
  ].filter(Boolean);
  return keys.join(mac ? "" : "+");
}
