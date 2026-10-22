import type { ReactNode } from "react";
import { LoaderCircleIcon } from "lucide-react";
import { AccountButton } from "./account.tsx";
import { ThemeButton } from "./ThemeButton.tsx";
import { Skeleton } from "@/components/ui/skeleton";
import { WorkspaceLoading } from "./WorkspaceLoading.tsx";
import { navigate } from "./identity-context.tsx";
import { Button } from "@/components/ui/button";
import { ContextIdentity } from "./ContextIdentity.tsx";
import { ProjectPicker } from "./ProjectPicker.tsx";
import { Navigation } from "./Navigation.tsx";
import {
  HeaderBrand,
  NewChangesButton,
  ProjectsToggle,
} from "./ProjectsNav.tsx";
import { useCompactChrome } from "./use-compact-chrome.ts";
import { useCatalog, useSessionShell } from "./session-context.tsx";

export function CurrentContext() {
  const catalog = useCatalog();
  return catalog.current ? <ContextIdentity current={catalog.current} /> : null;
}

/**
 * Presents the selected review once ready, or the loading, empty and failure
 * shell while SessionProvider resolves it. The picker spans both states.
 */
export function SessionShell({ children }: { children: ReactNode }) {
  const { current, selectedId, select, pickerOpen, heads } = useCatalog();
  const {
    ready,
    loading,
    empty,
    missing,
    statusError,
    collectError,
    collectOnLoad,
    collecting,
    remote,
    statusRef,
    retry,
    history,
    setPickerOpen,
  } = useSessionShell();
  const compact = useCompactChrome();
  return (
    <>
      <div className="app-shell">
        <div className="app-main">
          {ready ? (
            children
          ) : (
            <>
              <header className="topbar">
                {compact && <ProjectsToggle />}
                <HeaderBrand />
                <CurrentContext />
                {loading && !current && (
                  <Skeleton className="loading-identity" />
                )}
                <div className="header-actions">
                  <NewChangesButton />
                  {collecting && (
                    <Button
                      type="button"
                      id="sync"
                      variant="ghost"
                      size="icon"
                      aria-label="Sync all checkouts"
                      title="Syncing checkouts"
                      aria-busy="true"
                      disabled
                    >
                      <LoaderCircleIcon
                        className="size-(--icon-base)"
                        aria-hidden="true"
                      />
                    </Button>
                  )}
                  {compact && <AccountButton />}
                  <ThemeButton />
                </div>
              </header>
              <div className="workbench">
                <Navigation loading={loading} />
                <main
                  className={`session-status${loading ? " is-loading" : ""}`}
                  role="status"
                  aria-busy={loading}
                  ref={statusRef}
                  tabIndex={-1}
                >
                  {loading ? (
                    <WorkspaceLoading collecting={collectOnLoad} />
                  ) : (
                    <>
                      <h1>
                        {statusError
                          ? collectError
                            ? "Cannot sync changes"
                            : "Cannot load diffx"
                          : missing
                            ? "No current snapshot"
                            : empty
                              ? heads.contexts.length
                                ? "No changes to review"
                                : "No review contexts yet"
                              : "Loading diffx"}
                      </h1>
                      <p>
                        {statusError ||
                          (missing
                            ? "This stream's snapshots expired or were deleted. Pick current work from Repos."
                            : empty
                              ? remote
                                ? "Create a collector token in settings, then run diffx sync to publish your changes."
                                : "Run diffx to review a checkout, or diffx sync to publish remotely."
                              : "Reading review context…")}
                      </p>
                      {empty && remote && (
                        <Button
                          variant="outline"
                          onClick={() => navigate("/settings")}
                        >
                          Create a collector token
                        </Button>
                      )}
                      {statusError && (
                        <Button type="button" variant="outline" onClick={retry}>
                          Retry
                        </Button>
                      )}
                    </>
                  )}
                </main>
              </div>
            </>
          )}
        </div>
      </div>
      <ProjectPicker
        contexts={history.loaded ? history.contexts : heads.contexts}
        selectedId={selectedId}
        select={select}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
      />
    </>
  );
}
