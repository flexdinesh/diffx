import { useRef, type ReactNode } from "react";
import { FileTreeSkeleton } from "./WorkspaceLoading.tsx";
import { ProjectsNav } from "./ProjectsNav.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { useStreamNavState } from "./stream-nav-context.tsx";

/**
 * One navigation island: the review column, then the open review's file
 * sidebar. Each pane owns its width and resize boundary.
 */
export function Navigation({
  children,
  loading = false,
}: {
  children?: ReactNode;
  loading?: boolean;
}) {
  const sidebar = useSidebarState();
  const streams = useStreamNavState();
  const files = Boolean(children) || loading;
  const filesWidth = files && !sidebar.collapsed ? sidebar.width : 0;
  const rail = streams.state === "rail";
  return (
    <div className="navigation-island">
      <div className={`stream-column${rail ? " is-rail" : ""}`}>
        <ProjectsNav />
        <PaneResizer
          label="Resize reviews column"
          min={200}
          max={400}
          width={streams.width}
          hidden={rail || sidebar.mobile}
          onResize={(width) => streams.setWidth(width, filesWidth)}
        />
      </div>
      {files && (
        <div
          className="navigation-panel"
          hidden={!sidebar.mobile && sidebar.collapsed}
        >
          {loading && <FileTreeSkeleton />}
          {children}
          <PaneResizer
            label="Resize file sidebar"
            min={200}
            max={520}
            width={sidebar.width}
            hidden={sidebar.mobile}
            onResize={(width) =>
              sidebar.setWidth(width, rail ? 0 : streams.width)
            }
          />
        </div>
      )}
    </div>
  );
}

function PaneResizer({
  label,
  min,
  max,
  width,
  hidden,
  onResize,
}: {
  label: string;
  min: number;
  max: number;
  width: number;
  hidden: boolean;
  onResize: (width: number) => void;
}) {
  const drag = useRef({ x: 0, width });
  return (
    <hr
      className="sidebar-resizer"
      aria-label={label}
      aria-orientation="vertical"
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={width}
      tabIndex={0}
      hidden={hidden}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        event.preventDefault();
        drag.current = { x: event.clientX, width };
        event.currentTarget.setPointerCapture(event.pointerId);
        document.body.classList.add("resizing-sidebar");
      }}
      onPointerMove={(event) => {
        if (event.currentTarget.hasPointerCapture(event.pointerId)) {
          onResize(drag.current.width + event.clientX - drag.current.x);
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
          onResize(width + (event.key === "ArrowRight" ? 20 : -20));
        }
      }}
    />
  );
}
