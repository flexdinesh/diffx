import { useSyncExternalStore } from "react";

// Keep synchronized with the 768px compact-chrome breakpoint in the stylesheets.
const query = "(max-width: 767px)";

function subscribe(change: () => void) {
  const media = matchMedia(query);
  media.addEventListener("change", change);
  return () => media.removeEventListener("change", change);
}

/** Compact chrome hides the activity bar; its actions move into the header. */
export function useCompactChrome() {
  return useSyncExternalStore(subscribe, () => matchMedia(query).matches);
}
