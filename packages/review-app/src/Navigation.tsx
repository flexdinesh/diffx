import { useRef, type ReactNode } from "react";
import { FileTreeSkeleton } from "./WorkspaceLoading.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

/**
 * The open review's navigation pane: its file sidebar, with its width and
 * resize boundary. The pane hides, still mounted, while that sidebar is
 * collapsed. Switching reviews belongs to the Review bench and the header.
 */
export function Navigation({
  children,
  loading = false,
}: {
  children?: ReactNode;
  loading?: boolean;
}) {
  const sidebar = useSidebarState();
  const files = Boolean(children) || loading;
  const empty = !sidebar.mobile && (!files || sidebar.collapsed);
  return (
    <div className="navigation-island" data-empty={empty || undefined}>
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
            onResize={(width) => sidebar.setWidth(width)}
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
