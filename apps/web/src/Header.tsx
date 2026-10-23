import {
  FolderSyncIcon,
  ListTreeIcon,
  LoaderCircleIcon,
  RefreshCwIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { useDiffSource } from "./app-state.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { useCatalog, useSession } from "./session-context.tsx";
import { Topbar } from "./Topbar.tsx";
import { contextDiagnostics, observationSummary } from "./project-picker.ts";

export function Header() {
  const { repository, piped, diff, refresh, refreshEnabled } = useDiffSource();
  const session = useSession();
  const { observation } = session;
  const { collect, collecting } = useCatalog();
  const sidebar = useSidebarState();
  return (
    <Topbar
      lead={
        <Button
          type="button"
          id="sidebar-toggle"
          variant="ghost"
          size="icon"
          aria-controls="sidebar"
          aria-expanded={sidebar.view === "diff" && sidebar.expanded}
          aria-label={`${sidebar.view === "diff" && sidebar.expanded ? "Hide" : "Show"} file sidebar`}
          title={`${sidebar.view === "diff" && sidebar.expanded ? "Hide" : "Show"} file sidebar`}
          onClick={() => sidebar.selectView("diff")}
        >
          <ListTreeIcon className="size-(--icon-base)" />
        </Button>
      }
      actions={
        collect ? (
          <Button
            type="button"
            id="sync"
            variant="ghost"
            size="icon"
            aria-label="Sync all checkouts"
            title="Sync all checkouts (Alt+R)"
            aria-busy={collecting || diff.busy}
            onClick={refresh}
          >
            {collecting || diff.busy ? (
              <LoaderCircleIcon
                className="size-(--icon-base)"
                aria-hidden="true"
              />
            ) : (
              <FolderSyncIcon
                className="size-(--icon-base)"
                aria-hidden="true"
              />
            )}
          </Button>
        ) : (
          <Button
            type="button"
            id="refresh"
            variant="ghost"
            aria-label={
              refreshEnabled ? "Refresh changes" : "Retry loading changes"
            }
            title={
              refreshEnabled
                ? "Refresh changes (Alt+R)"
                : "Retry loading stored changes"
            }
            hidden={!refreshEnabled && !diff.notice}
            aria-busy={diff.busy}
            onClick={refresh}
          >
            <RefreshCwIcon className="size-(--icon-base)" aria-hidden="true" />
            <span className="refresh-label">
              {refreshEnabled ? "Refresh" : "Retry"}
            </span>
          </Button>
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
