import { useCallback, useEffect, useState } from "react";
import { save, saved } from "./preferences.ts";
import { useModalDrawer } from "./use-modal-drawer.ts";

// React owns both the desktop preferences and the temporary mobile overlay.
export function useSidebar() {
  const [mobile, setMobile] = useState(
    () => matchMedia("(max-width: 1011px)").matches,
  );
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
  useModalDrawer(mobile && open, "#sidebar", "#sidebar-toggle");
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
    if (mobile) setOpen(true);
    else setCollapsed(false);
  }, [mobile]);
  const closeMobile = useCallback(() => setOpen(false), []);
  return {
    mobile,
    open,
    collapsed,
    width,
    show,
    closeMobile,
    expanded: mobile ? open : !collapsed,
    toggle: () => {
      if (mobile) setOpen((value) => !value);
      else setCollapsed((value) => !value);
    },
    setWidth: (next: number) =>
      setWidthState(
        Math.max(200, Math.min(520, window.innerWidth - 320, next)),
      ),
  };
}
