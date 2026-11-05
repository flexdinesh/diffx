import { useCallback, useEffect, useState } from "react";
import { save, saved } from "@diffx/ui/lib/storage";
import type { StreamGroupMode } from "./project-nav.ts";

/** The desktop review column: a repository rail or the stream list. */
export type StreamColumnState = "rail" | "list";

// One owner for the review column's desktop state. Mobile drawers stay with
// use-sidebar.ts.
export function useStreamNav() {
  const [state, setState] = useState<StreamColumnState>(() =>
    saved("streams-column") === "rail" ? "rail" : "list",
  );
  const [width, setWidthState] = useState(() => {
    const width = Number(saved("streams-width"));
    return width >= 200 && width <= 400 ? width : 240;
  });
  const [group, setGroup] = useState<StreamGroupMode>(() =>
    saved("streams-group") === "recent" ? "recent" : "repo",
  );
  useEffect(() => {
    save("streams-column", state);
  }, [state]);
  useEffect(() => {
    save("streams-group", group);
  }, [group]);
  useEffect(() => {
    save("streams-width", String(width));
    document.documentElement.style.setProperty("--streams-width", `${width}px`);
  }, [width]);
  const expand = useCallback(() => setState("list"), []);
  const collapse = useCallback(() => setState("rail"), []);
  return {
    state,
    width,
    group,
    setGroup,
    expand,
    collapse,
    /** `reserved` is the file sidebar's width, kept beside a 320px diff. */
    setWidth: (next: number, reserved: number) =>
      setWidthState(
        Math.max(200, Math.min(400, window.innerWidth - 320 - reserved, next)),
      ),
  };
}
