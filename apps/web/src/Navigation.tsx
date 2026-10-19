import { useRef, type ReactNode } from "react";
import { FileTreeSkeleton } from "./WorkspaceLoading.tsx";
import { ActivityBar } from "./ActivityBar.tsx";
import { ProjectsNav } from "./ProjectsNav.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

/** The shared navigation frame owns panel width and its resize boundary. */
export function Navigation({
  children,
  loading = false,
}: {
  children?: ReactNode;
  loading?: boolean;
}) {
  const sidebar = useSidebarState();
  const diffReady = Boolean(children);
  const expanded =
    sidebar.expanded && (sidebar.view !== "diff" || diffReady || loading);
  return (
    <div className="navigation-island">
      <ActivityBar diffReady={diffReady} />
      <div className="navigation-panel" hidden={!expanded}>
        <ProjectsNav />
        {loading && <FileTreeSkeleton />}
        {children}
      </div>
      <NavigationResizer hidden={!expanded || sidebar.mobile} />
    </div>
  );
}

function NavigationResizer({ hidden }: { hidden: boolean }) {
  const sidebar = useSidebarState();
  const drag = useRef({ x: 0, width: sidebar.width });
  return (
    <hr
      id="sidebar-resizer"
      aria-label="Resize navigation panel"
      aria-orientation="vertical"
      aria-valuemin={200}
      aria-valuemax={520}
      aria-valuenow={sidebar.width}
      tabIndex={0}
      hidden={hidden}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        drag.current = { x: event.clientX, width: sidebar.width };
        event.currentTarget.setPointerCapture(event.pointerId);
        document.body.classList.add("resizing-sidebar");
      }}
      onPointerMove={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          sidebar.setWidth(drag.current.width + event.clientX - drag.current.x);
        }
      }}
      onPointerUp={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId))
          event.currentTarget.releasePointerCapture(event.pointerId);
      }}
      onLostPointerCapture={() =>
        document.body.classList.remove("resizing-sidebar")
      }
      onKeyDown={(event) => {
        if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
          event.preventDefault();
          sidebar.setWidth(
            sidebar.width + (event.key === "ArrowRight" ? 20 : -20),
          );
        }
      }}
    />
  );
}
