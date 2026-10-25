import type { ReactNode } from "react";
import { EmptyState } from "@/components/empty-state";
import { Skeleton } from "@/components/ui/skeleton";
import { WorkspaceLoading } from "./WorkspaceLoading.tsx";
import { navigate } from "./identity-context.tsx";
import { Button } from "@/components/ui/button";
import { ProjectPicker } from "./ProjectPicker.tsx";
import { Navigation } from "./Navigation.tsx";
import { SyncButton, Topbar } from "./Topbar.tsx";
import { useCatalog, useSessionShell } from "./session-context.tsx";
import { useShellPolicy } from "./shell-policy.ts";

/**
 * Presents the selected review once ready, or the loading, empty and failure
 * shell while SessionProvider resolves it. The picker spans both states
 * wherever the shell offers review navigation.
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
  const { reviewNavigation } = useShellPolicy();
  return (
    <>
      <div className="app-shell">
        <div className="app-main">
          {ready ? (
            children
          ) : (
            <>
              <Topbar actions={collecting && <SyncButton busy />}>
                {loading && !current && (
                  <Skeleton className="ml-3 w-36 max-md:hidden" />
                )}
              </Topbar>
              <div className="workbench">
                <Navigation loading={loading} />
                <div className="workspace">
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
                      <EmptyState
                        size="page"
                        heading="h1"
                        title={
                          statusError
                            ? collectError
                              ? "Cannot sync changes"
                              : "Cannot load diffx"
                            : missing
                              ? "No current snapshot"
                              : empty
                                ? heads.contexts.length
                                  ? "No changes to review"
                                  : "No review contexts yet"
                                : "Loading diffx"
                        }
                        description={
                          statusError ||
                          (missing
                            ? "This stream's snapshots expired or were deleted. Pick current work from Reviews."
                            : empty
                              ? remote
                                ? "Create a collector token in settings, then run diffx sync to publish your changes."
                                : "Run diffx to review a checkout, or diffx sync to publish remotely."
                              : "Reading review context…")
                        }
                        action={
                          statusError || (empty && remote) ? (
                            <>
                              {empty && remote && (
                                <Button
                                  type="button"
                                  variant="outline"
                                  onClick={() => navigate("/settings")}
                                >
                                  Create a collector token
                                </Button>
                              )}
                              {statusError && (
                                <Button
                                  type="button"
                                  variant="outline"
                                  onClick={retry}
                                >
                                  Retry
                                </Button>
                              )}
                            </>
                          ) : undefined
                        }
                      />
                    )}
                  </main>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
      {reviewNavigation && (
        <ProjectPicker
          contexts={history.loaded ? history.contexts : heads.contexts}
          selectedId={selectedId}
          select={select}
          open={pickerOpen}
          onOpenChange={setPickerOpen}
        />
      )}
    </>
  );
}
