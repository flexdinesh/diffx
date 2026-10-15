import { useEffect } from "react";

// Below the drawer breakpoint, navigation panels are modal: the page behind
// them is inert, focus stays inside, and closing restores focus to the toggle.
export function useModalDrawer(
  active: boolean,
  drawerSelector: string,
  toggleSelector: string,
) {
  useEffect(() => {
    if (!active) return;
    const main = document.querySelector<HTMLElement>("main");
    const header = document.querySelector<HTMLElement>(".topbar");
    const views = document.querySelector<HTMLElement>(".activity-bar");
    const drawer = document.querySelector<HTMLElement>(drawerSelector);
    main?.setAttribute("inert", "");
    header?.setAttribute("inert", "");
    views?.setAttribute("inert", "");
    const focusable = () =>
      Array.from(
        drawer?.querySelectorAll<HTMLElement>(
          'button:not([disabled]), input:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
        ) ?? [],
      );
    requestAnimationFrame(() => focusable()[0]?.focus());
    const trapFocus = (event: KeyboardEvent) => {
      if (event.key !== "Tab") return;
      const elements = focusable();
      const first = elements[0];
      const last = elements.at(-1);
      if (!first || !last) return;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", trapFocus);
    return () => {
      document.removeEventListener("keydown", trapFocus);
      main?.removeAttribute("inert");
      header?.removeAttribute("inert");
      views?.removeAttribute("inert");
      document.querySelector<HTMLElement>(toggleSelector)?.focus();
    };
  }, [active, drawerSelector, toggleSelector]);
}
