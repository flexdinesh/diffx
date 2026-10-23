import { Trash2Icon } from "lucide-react";
import { useRef, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogTitle,
} from "@/components/ui/dialog";

/**
 * Confirms a destructive action. Cancel takes initial focus; the confirm is
 * the only filled red control in the app.
 */
export function ConfirmDialog({
  open,
  onOpenChange,
  trigger,
  title,
  description,
  details,
  error,
  confirmLabel,
  busyLabel,
  busy = false,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** A DialogTrigger, when the dialog owns its opener. */
  trigger?: ReactNode;
  title: string;
  description: ReactNode;
  /** Identifies what will be deleted. */
  details?: ReactNode;
  error?: string;
  confirmLabel: string;
  busyLabel?: string;
  busy?: boolean;
  onConfirm: () => void;
}) {
  const cancel = useRef<HTMLButtonElement>(null);
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!busy) onOpenChange(value);
      }}
    >
      {trigger}
      <DialogContent initialFocus={() => cancel.current}>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
        {details}
        {error && (
          <p role="alert" className="m-0 text-sm text-error">
            {error}
          </p>
        )}
        <DialogFooter>
          <Button
            ref={cancel}
            type="button"
            variant="outline"
            touch
            disabled={busy}
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive-solid"
            touch
            disabled={busy}
            aria-busy={busy}
            onClick={onConfirm}
          >
            <Trash2Icon aria-hidden="true" />
            {busy && busyLabel ? busyLabel : confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
