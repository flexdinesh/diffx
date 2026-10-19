import { FolderSyncIcon, ListTreeIcon, RefreshCwIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { AccountButton } from "./account.tsx";
import { ThemeButton } from "./ThemeButton.tsx";
import {
  HeaderBrand,
  NewChangesButton,
  ProjectsToggle,
} from "./ProjectsNav.tsx";
import { useDiffSource } from "./app-state.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { CurrentContext, useCatalog, useSession } from "./session-context.tsx";
import { contextDiagnostics, observationSummary } from "./project-picker.ts";
import { useCompactChrome } from "./use-compact-chrome.ts";

export function Header() {
  const { repository, piped, diff, refresh, refreshEnabled } = useDiffSource();
  const session = useSession();
  const { observation } = session;
  const { collect, collecting } = useCatalog();
  const compact = useCompactChrome();
  const sidebar = useSidebarState();
  return (
    <header className="topbar">
      {compact && <ProjectsToggle />}
      <HeaderBrand />
      {compact && (
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
      )}
      <CurrentContext />
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
      <div className="header-actions">
        <NewChangesButton />
        {collect ? (
          <Button
            type="button"
            id="sync"
            variant="ghost"
            size="icon"
            aria-label="Sync all checkouts"
            title="Sync all checkouts (Alt+R)"
            aria-busy={collecting}
            onClick={refresh}
          >
            <FolderSyncIcon className="size-(--icon-base)" aria-hidden="true" />
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
        )}
        {compact && <AccountButton />}
        <ThemeButton />
      </div>
    </header>
  );
}
