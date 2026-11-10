import { useEffect, useState } from "react";
import { save, saved } from "@diffx/ui/lib/storage";

export const benchPeekWidth = { min: 320, max: 640, initial: 400 };

/** Facets (240px) and the narrowest review table (480px) beside the peek. */
const besidePeek = 240 + 480;

// Owns the Review bench peek's desktop width.
export function useBenchPeekWidth() {
  const { min, max, initial } = benchPeekWidth;
  const [width, setWidthState] = useState(() => {
    const width = Number(saved("bench-peek-width"));
    return width >= min && width <= max ? width : initial;
  });
  useEffect(() => {
    save("bench-peek-width", String(width));
    document.documentElement.style.setProperty(
      "--bench-peek-width",
      `${width}px`,
    );
  }, [width]);
  return {
    width,
    /** Keeps the review table at least 480px wide beside the peek. */
    setWidth: (next: number) =>
      setWidthState(
        Math.max(min, Math.min(max, window.innerWidth - besidePeek, next)),
      ),
    reset: () => setWidthState(initial),
  };
}
