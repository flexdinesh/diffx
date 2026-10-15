import { ListTreeIcon, RefreshCwIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { ShellActions } from "./ActivityBar.tsx";
import { DeleteContextButton } from "./DeleteContextButton.tsx";
import { ProjectsToggle } from "./ProjectsNav.tsx";
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
      <ProjectsToggle />
      <Button
        type="button"
        id="sidebar-toggle"
        variant="ghost"
        size="icon"
        aria-controls="sidebar"
        aria-expanded={sidebar.expanded}
        aria-label={`${sidebar.expanded ? "Hide" : "Show"} file sidebar`}
        title={`${sidebar.expanded ? "Hide" : "Show"} file sidebar`}
        onClick={sidebar.toggle}
      >
        <ListTreeIcon className="size-(--icon-base)" />
      </Button>
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
        <DeleteContextButton />
        <Button
          type="button"
          id="refresh"
          variant="ghost"
          aria-label={
            refreshEnabled ? "Refresh changes" : "Retry loading changes"
          }
          title={
            collect
              ? "Collect the latest changes from this checkout (Alt+R)"
              : refreshEnabled
                ? "Refresh changes (Alt+R)"
                : "Retry loading stored changes"
          }
          hidden={!refreshEnabled && !diff.notice}
          aria-busy={collecting || diff.busy}
          onClick={refresh}
        >
          <RefreshCwIcon className="size-(--icon-base)" aria-hidden="true" />
          <span className="refresh-label">
            {refreshEnabled ? "Refresh" : "Retry"}
          </span>
        </Button>
        {compact && <ShellActions side="bottom" />}
      </div>
    </header>
  );
}
