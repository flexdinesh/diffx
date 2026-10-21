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
    // The background can remount while the drawer stays open (opening a
    // review replaces <main>), so keep whichever elements are current inert.
    const background = () =>
      document.querySelectorAll<HTMLElement>("main, .topbar, .activity-bar");
    const makeInert = () => {
      for (const element of background())
        if (!element.hasAttribute("inert")) element.setAttribute("inert", "");
    };
    const drawer = document.querySelector<HTMLElement>(drawerSelector);
    makeInert();
    const remounts = new MutationObserver(makeInert);
    remounts.observe(document.body, { childList: true, subtree: true });
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
      remounts.disconnect();
      for (const element of background()) element.removeAttribute("inert");
      document.querySelector<HTMLElement>(toggleSelector)?.focus();
    };
  }, [active, drawerSelector, toggleSelector]);
}
