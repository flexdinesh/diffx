import { useRef, useState } from "react";
import { Trash2Icon } from "lucide-react";
import { errorDetail } from "@diffx/api";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { IconButton } from "@diffx/ui/components/icon-button";
import { DialogTrigger } from "@diffx/ui/components/ui/dialog";
import { useDeleteContext, useSession } from "./session-context.tsx";
import { contextDiagnostics } from "./project-picker.ts";

export function DeleteContextButton() {
  const session = useSession();
  const deleteContext = useDeleteContext();
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const pending = useRef(false);
  const label = "Delete snapshot";

  async function confirm() {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    try {
      await deleteContext(session.id);
      setOpen(false);
    } catch (error: unknown) {
      setError(errorDetail(error, "Unable to delete. Try again."));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(value) => {
        if (pending.current) return;
        setError("");
        setOpen(value);
      }}
      trigger={
        <DialogTrigger
          render={
            <IconButton label={label} touch>
              <Trash2Icon aria-hidden="true" />
            </IconButton>
          }
        />
      }
      title={`${label}?`}
      description="This permanently deletes this snapshot, including comments and reviewed-file marks. This cannot be undone. Your Git files stay unchanged. New collections can appear again."
      details={
        <p
          className="flex flex-col gap-1 text-sm text-secondary [overflow-wrap:anywhere]"
          title={contextDiagnostics(session)}
        >
          <strong className="text-fg">{session.name}</strong>
          {session.branch && <span>{session.branch}</span>}
          {session.observation ? (
            <span>
              {session.observation.hostname} ·{" "}
              {new Date(session.observation.collectedAt).toLocaleString()}
            </span>
          ) : session.root ? (
            <span>{session.root}</span>
          ) : null}
        </p>
      }
      error={error}
      confirmLabel={label}
      busyLabel="Deleting…"
      busy={busy}
      onConfirm={() => void confirm()}
    />
  );
}
