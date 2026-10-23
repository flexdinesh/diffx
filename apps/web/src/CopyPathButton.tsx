import { CheckIcon, CopyIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { IconButton } from "@/components/icon-button";
import { cn } from "@/lib/utils";

export function CopyPathButton({
  value,
  label,
}: {
  value: string;
  label: string;
}) {
  const [status, setStatus] = useState<"idle" | "copying" | "copied" | "error">(
    "idle",
  );
  const feedback =
    status === "copied" ? "Copied" : status === "error" ? "Copy failed" : "";

  useEffect(() => {
    if (status !== "copied" && status !== "error") return;
    const timeout = window.setTimeout(() => setStatus("idle"), 2_000);
    return () => window.clearTimeout(timeout);
  }, [status]);

  async function copy() {
    setStatus("copying");
    try {
      await navigator.clipboard.writeText(value);
      setStatus("copied");
    } catch {
      setStatus("error");
    }
  }

  return (
    <span className="inline-flex flex-none items-center gap-1">
      <IconButton
        label={`${label}: ${value}`}
        hint={feedback || label}
        size="icon-xs"
        aria-busy={status === "copying"}
        disabled={status === "copying"}
        onClick={copy}
      >
        {status === "copied" ? (
          <CheckIcon aria-hidden="true" />
        ) : (
          <CopyIcon aria-hidden="true" />
        )}
      </IconButton>
      <span
        className={cn(
          "font-sans text-xs leading-ui",
          status === "error" ? "text-error" : "text-success",
        )}
        role="status"
      >
        {feedback}
      </span>
    </span>
  );
}
