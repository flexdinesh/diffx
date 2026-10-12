import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { save, saved } from "./preferences.ts";
import { useModalDrawer } from "./use-modal-drawer.ts";

// Desktop keeps a persisted collapsible column; below 1012px the drawer is modal.
function useProjectsNav() {
  const [mobile, setMobile] = useState(
    () => matchMedia("(max-width: 1011px)").matches,
  );
  const [open, setOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(
    () => saved("projects-collapsed") === "true",
  );
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
  useModalDrawer(mobile && open, "#projects-nav", "#projects-toggle");
  useEffect(() => {
    save("projects-collapsed", String(collapsed));
  }, [collapsed]);
  const toggle = useCallback(() => {
    if (mobile) setOpen((value) => !value);
    else setCollapsed((value) => !value);
  }, [mobile]);
  const closeMobile = useCallback(() => setOpen(false), []);
  return {
    mobile,
    open,
    expanded: mobile ? open : !collapsed,
    toggle,
    closeMobile,
  };
}

const ProjectsNavContext = createContext<ReturnType<
  typeof useProjectsNav
> | null>(null);

export function ProjectsNavProvider({ children }: { children: ReactNode }) {
  const projects = useProjectsNav();
  return <ProjectsNavContext value={projects}>{children}</ProjectsNavContext>;
}

export function useProjectsNavState() {
  const projects = useContext(ProjectsNavContext);
  if (!projects) throw new Error("Page components require ProjectsNavProvider");
  return projects;
}
