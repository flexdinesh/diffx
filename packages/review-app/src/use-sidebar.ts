import { useCallback, useEffect, useState } from "react";
import { save, saved } from "@diffx/ui/lib/storage";
import { useModalDrawer } from "./use-modal-drawer.ts";

/** Below the 1012px breakpoint, the file sidebar opens as a modal drawer. */
export type NavigationDrawer = "files";

// Owns the file sidebar's desktop preferences and its mobile drawer.
export function useSidebar() {
  const [mobile, setMobile] = useState(
    () => matchMedia("(max-width: 1011px)").matches,
  );
  const [drawer, setDrawer] = useState<NavigationDrawer | null>(null);
  const [collapsed, setCollapsed] = useState(
    () => saved("sidebar-collapsed") === "true",
  );
  const [width, setWidthState] = useState(() => {
    const width = Number(saved("sidebar-width"));
    return width >= 200 && width <= 520 ? width : 330;
  });
  useEffect(() => {
    const media = matchMedia("(max-width: 1011px)");
    const update = () => {
      setMobile(media.matches);
      setDrawer(null);
    };
    media.addEventListener("change", update);
    const onEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setDrawer(null);
    };
    document.addEventListener("keydown", onEscape);
    return () => {
      media.removeEventListener("change", update);
      document.removeEventListener("keydown", onEscape);
    };
  }, []);
  useModalDrawer(mobile && drawer !== null, "#sidebar", "#sidebar-toggle");
  useEffect(() => {
    save("sidebar-collapsed", String(collapsed));
  }, [collapsed]);
  useEffect(() => {
    save("sidebar-width", String(width));
    document.documentElement.style.setProperty("--sidebar-width", `${width}px`);
  }, [width]);
  useEffect(
    () => () => {
      document.body.classList.remove("resizing-sidebar");
    },
    [],
  );
  const show = useCallback(() => {
    if (mobile) setDrawer("files");
    else setCollapsed(false);
  }, [mobile]);
  // Opening a review shows its diff: any drawer closes; desktop panes stay.
  const openDiff = useCallback(() => setDrawer(null), []);
  const closeMobile = useCallback(() => setDrawer(null), []);
  return {
    mobile,
    drawer: mobile ? drawer : null,
    collapsed,
    width,
    show,
    closeMobile,
    openDiff,
    /** Whether the file sidebar is visible: its drawer, or the desktop pane. */
    expanded: mobile ? drawer === "files" : !collapsed,
    toggleFiles: () => {
      if (mobile) setDrawer((value) => (value === "files" ? null : "files"));
      else setCollapsed((value) => !value);
    },
    /** Keeps the diff at least 320px wide beside the sidebar. */
    setWidth: (next: number) =>
      setWidthState(
        Math.max(200, Math.min(520, window.innerWidth - 320, next)),
      ),
  };
}
