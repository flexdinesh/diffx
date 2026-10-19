import { useCallback, useEffect, useState } from "react";
import { save, saved } from "./preferences.ts";
import { useModalDrawer } from "./use-modal-drawer.ts";

export type WorkspaceView = "projects" | "diff";

// One owner for workspace views, desktop preferences and mobile overlays.
export function useSidebar() {
  const [mobile, setMobile] = useState(
    () => matchMedia("(max-width: 1011px)").matches,
  );
  const [view, setView] = useState<WorkspaceView>("diff");
  const [open, setOpen] = useState(false);
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
      setOpen(false);
    };
    media.addEventListener("change", update);
    const onEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("keydown", onEscape);
    return () => {
      media.removeEventListener("change", update);
      document.removeEventListener("keydown", onEscape);
    };
  }, []);
  useModalDrawer(
    mobile && open,
    view === "projects" ? "#projects-nav" : "#sidebar",
    view === "projects"
      ? "#projects-toggle, #projects-view"
      : "#sidebar-toggle, #diff-view",
  );
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
    setView("diff");
    if (mobile) setOpen(true);
    else setCollapsed(false);
  }, [mobile]);
  const openDiff = useCallback(() => {
    setView("diff");
    setOpen(false);
    setCollapsed(false);
  }, []);
  const closeMobile = useCallback(() => setOpen(false), []);
  return {
    mobile,
    view,
    open,
    collapsed,
    width,
    show,
    closeMobile,
    expanded: mobile ? open : !collapsed,
    selectView: (next: WorkspaceView) => {
      setView(next);
      if (mobile) setOpen(view === next ? !open : true);
      else setCollapsed(view === next ? !collapsed : false);
    },
    openDiff,
    setWidth: (next: number) =>
      setWidthState(
        Math.max(200, Math.min(520, window.innerWidth - 320, next)),
      ),
  };
}
