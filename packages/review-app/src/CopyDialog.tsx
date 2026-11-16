import { useRef } from "react";
import { Button } from "@diffx/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@diffx/ui/components/ui/dialog";
import { Textarea } from "@diffx/ui/components/ui/textarea";
import { formatShortcut } from "@diffx/ui/lib/format";

/**
 * Confirms copied content, or shows it selected for manual copying when the
 * clipboard refused it.
 */
export function CopyDialog({
  content,
  onClose,
}: {
  content: {
    clipboard: "copied" | "manual";
    label: string;
    text: string;
    title: string;
  };
  onClose: () => void;
}) {
  const input = useRef<HTMLTextAreaElement>(null);
  return (
    <Dialog
      open
      onOpenChange={(open) => !open && onClose()}
      onOpenChangeComplete={(open) => {
        if (open && content.clipboard === "manual") input.current?.select();
      }}
    >
      <DialogContent
        initialFocus={content.clipboard === "manual" ? input : undefined}
      >
        <DialogTitle id="copy-dialog-title">{content.title}</DialogTitle>
        <DialogDescription>
          {content.clipboard === "copied"
            ? "Content copied to clipboard."
            : `Copy the selected text with ${formatShortcut({ mod: true, key: "C" })}.`}
        </DialogDescription>
        {content.clipboard === "manual" && (
          <Textarea
            ref={input}
            variant="code"
            rows={15}
            value={content.text}
            readOnly
            aria-label={content.label}
          />
        )}
        <Button type="button" variant="outline" touch onClick={onClose}>
          Close
        </Button>
      </DialogContent>
    </Dialog>
  );
}
