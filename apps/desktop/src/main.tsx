import { useCallback, useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { ReviewApplication } from "@diffx/review-app";
import { EmptyState } from "@diffx/ui/components/empty-state";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@diffx/ui/components/ui/dialog";
import { Button } from "@diffx/ui/components/ui/button";
import { desktopCall, type LaunchStatus } from "./bridge.ts";

function DesktopApplication() {
  const [status, setStatus] = useState<LaunchStatus>({
    phase: "starting",
    generation: 0,
    error: "",
    owned: false,
    canReplace: false,
    confirmSwitch: false,
    destination: "",
  });
  const [error, setError] = useState("");
  const [confirmReplace, setConfirmReplace] = useState(false);
  const [busy, setBusy] = useState(false);
  const switchCancel = useRef<HTMLButtonElement>(null);
  const bootstrapped = useRef(false);
  const generation = useRef<number | null>(null);
  const receive = useCallback((next: LaunchStatus) => {
    if (generation.current !== null && next.generation < generation.current)
      return;
    if (generation.current !== null && next.generation !== generation.current)
      window.history.replaceState(null, "", "/");
    generation.current = next.generation;
    setStatus(next);
  }, []);

  useEffect(() => {
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    async function refresh() {
      try {
        const next = await desktopCall(
          bootstrapped.current ? "Desktop.Status" : "Desktop.Bootstrap",
        );
        bootstrapped.current = true;
        if (active) receive(next);
      } catch (cause) {
        if (active)
          setError(
            cause instanceof Error
              ? cause.message
              : "Desktop connection unavailable.",
          );
      }
      if (active) timer = setTimeout(() => void refresh(), 250);
    }
    void refresh();
    return () => {
      active = false;
      clearTimeout(timer);
    };
  }, [receive]);

  const action = useCallback(
    async (
      method:
        | "Desktop.Retry"
        | "Desktop.Folder"
        | "Desktop.Replace"
        | "Desktop.Confirm"
        | "Desktop.Cancel",
    ) => {
      setBusy(true);
      setError("");
      setConfirmReplace(false);
      try {
        const next = await desktopCall(method);
        // Keyed shared state disposes previous requests before the new scope.
        receive(next);
      } catch (cause) {
        setError(
          cause instanceof Error
            ? cause.message
            : "Could not open the local review.",
        );
      } finally {
        setBusy(false);
      }
    },
    [receive],
  );

  useEffect(() => {
    const choose = () => void action("Desktop.Folder");
    window.addEventListener("diffx:choose-folder", choose);
    return () => window.removeEventListener("diffx:choose-folder", choose);
  }, [action]);

  const review = status.phase === "ready" || status.phase === "reconnecting";
  const starting = status.phase === "starting" && !error;
  return (
    <>
      {review ? (
        <ReviewApplication key={status.generation} platform="desktop" />
      ) : (
        <main className="grid h-dvh place-items-center bg-bg text-fg">
          <EmptyState
            size="page"
            heading="h1"
            title={
              starting
                ? "Opening local review"
                : confirmReplace
                  ? "Replace running local session?"
                  : status.phase === "folder"
                    ? "Choose a checkout or workspace"
                    : "Could not open local review"
            }
            description={
              confirmReplace
                ? "The running local server will stop. Its browser sessions will disconnect. Saved reviews and comments remain available."
                : error ||
                  status.error ||
                  "Starting the matching local Diffx server."
            }
            role={starting ? "status" : "alert"}
            action={
              confirmReplace ? (
                <>
                  <Button
                    variant="destructive-solid"
                    disabled={busy}
                    onClick={() => void action("Desktop.Replace")}
                  >
                    Replace session
                  </Button>
                  <Button
                    variant="outline"
                    onClick={() => setConfirmReplace(false)}
                  >
                    Cancel
                  </Button>
                </>
              ) : (
                <>
                  {!starting && (
                    <Button
                      disabled={busy}
                      onClick={() => void action("Desktop.Retry")}
                    >
                      Retry
                    </Button>
                  )}
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => void action("Desktop.Folder")}
                  >
                    Choose folder
                  </Button>
                  {status.canReplace && (
                    <Button
                      variant="destructive"
                      disabled={busy}
                      onClick={() => setConfirmReplace(true)}
                    >
                      Replace running session…
                    </Button>
                  )}
                </>
              )
            }
          />
        </main>
      )}
      {status.phase === "reconnecting" && (
        <p
          role="status"
          className="fixed bottom-0 left-0 m-0 bg-panel p-4 text-sm text-fg"
        >
          Reconnecting… Your current review remains open.
        </p>
      )}
      {review && error && (
        <p
          role="alert"
          className="fixed bottom-0 left-0 m-0 bg-panel p-4 text-sm text-error"
        >
          {error}
        </p>
      )}
      <Dialog
        open={status.confirmSwitch}
        onOpenChange={(open) => {
          if (!open && !busy) void action("Desktop.Cancel");
        }}
      >
        <DialogContent initialFocus={() => switchCancel.current}>
          <DialogTitle>Switch workspace or connection?</DialogTitle>
          <DialogDescription>
            Open {status.destination || "the requested session"}? The current
            review will close. Saved comments remain available. Cancel keeps
            this session open.
          </DialogDescription>
          <DialogFooter>
            <Button
              ref={switchCancel}
              variant="outline"
              disabled={busy}
              onClick={() => void action("Desktop.Cancel")}
            >
              Cancel
            </Button>
            <Button
              disabled={busy}
              onClick={() => void action("Desktop.Confirm")}
            >
              Switch session
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

const root = document.getElementById("root");
if (root) createRoot(root).render(<DesktopApplication />);
