import type { ReactNode } from "react";
import { PaneResizer } from "./PaneResizer.tsx";
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
