import { useSyncExternalStore } from "react";

// Keep synchronized with the 768px compact-chrome breakpoint in the stylesheets.
const query = "(max-width: 767px)";

function subscribe(change: () => void) {
  const media = matchMedia(query);
  media.addEventListener("change", change);
  return () => media.removeEventListener("change", change);
}

/** Compact chrome drops visible action text and enlarges header targets. */
export function useCompactChrome() {
  return useSyncExternalStore(subscribe, () => matchMedia(query).matches);
}
