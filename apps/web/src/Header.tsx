import { ListTreeIcon, RefreshCwIcon } from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { useDiffSource } from "./app-state.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { useCatalog, useSession } from "./session-context.tsx";
import { SyncButton, Topbar } from "./Topbar.tsx";
import { contextDiagnostics, observationSummary } from "./project-picker.ts";
import { useCompactChrome } from "./use-compact-chrome.ts";

export function Header() {
  const { repository, piped, diff, refresh, refreshEnabled } = useDiffSource();
  const session = useSession();
  const { observation } = session;
  const { collect, collecting } = useCatalog();
  const sidebar = useSidebarState();
  const compact = useCompactChrome();
  const sidebarOpen = sidebar.view === "diff" && sidebar.expanded;
  return (
    <Topbar
      lead={
        <IconButton
          id="sidebar-toggle"
          variant="shell"
          touch
          label={`${sidebarOpen ? "Hide" : "Show"} file sidebar`}
          aria-controls="sidebar"
          aria-expanded={sidebarOpen}
          onClick={() => sidebar.selectView("diff")}
        >
          <ListTreeIcon aria-hidden="true" />
        </IconButton>
      }
      actions={
        collect ? (
          <SyncButton busy={collecting || diff.busy} onClick={refresh} />
        ) : (
          // Compact chrome drops the visible text, leaving a 44px icon target.
          <IconButton
            variant="shell"
            size={compact ? "icon" : "default"}
            touch
            label={refreshEnabled ? "Refresh changes" : "Retry loading changes"}
            {...(refreshEnabled && { shortcut: { alt: true, key: "R" } })}
            hidden={!refreshEnabled && !diff.notice}
            aria-busy={diff.busy}
            onClick={refresh}
          >
            <RefreshCwIcon
              className={diff.busy ? "motion-safe:animate-spinner" : undefined}
              aria-hidden="true"
            />
            {!compact && (refreshEnabled ? "Refresh" : "Retry")}
          </IconButton>
        )
      }
    >
      <div className="header-heading">
        <h1
          id="changes-title"
          className={piped || observation ? "sr-only" : undefined}
        >
          {piped ? "Piped" : observation ? "Changes" : "Local changes"}
        </h1>
        <p
          id="repo-path"
          className="header-path"
          title={
            observation
              ? contextDiagnostics(session)
              : piped
                ? "Re-run your command to update"
                : repository?.root
          }
        >
          {piped
            ? "From stdin · Git unchanged"
            : observation
              ? observationSummary(session)
              : (repository?.root ?? "Reading your repository…")}
        </p>
      </div>
    </Topbar>
  );
}
