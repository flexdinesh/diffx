import { errorDetail } from "@diffx/console-api";
import { useEffect, useState, type ReactNode } from "react";
import { Button } from "@diffx/ui/components/ui/button";

export const prose = "mb-4 leading-copy text-secondary";
export const row =
  "flex flex-wrap items-center justify-between gap-4 border-b border-separator py-3";

export function date(value: number) {
  return new Date(value).toLocaleDateString();
}

/** A signed-in page: its title, a short description, then its sections. */
export function Page({
  title,
  description,
  children,
}: {
  title: string;
  description: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <h1 className="mb-2 text-xl font-semibold">{title}</h1>
      <p className={prose}>{description}</p>
      {children}
    </>
  );
}

/** Loads a list and reloads it on demand, keeping the last list meanwhile. */
export function useList<T>(
  load: (signal: AbortSignal) => Promise<T[]>,
  failure: string,
) {
  const [items, setItems] = useState<T[] | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    load(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setItems(value);
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorDetail(cause, failure));
      });
    return () => controller.abort();
  }, [load, failure, attempt]);
  return { items, error, reload: () => setAttempt((value) => value + 1) };
}

/**
 * A secret link shown once, after it was created. It is never listed again,
 * so the panel says so and offers Copy.
 */
export function OneTimeLink({
  title,
  children,
  url,
  onDismiss,
}: {
  title: string;
  children: ReactNode;
  url: string;
  onDismiss: () => void;
}) {
  const [copyStatus, setCopyStatus] = useState("");
  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      setCopyStatus("Copied");
    } catch {
      setCopyStatus("Copy unavailable. Select and copy the link above.");
    }
  }
  return (
    <div
      className="mb-6 rounded-control border border-separator bg-panel p-4"
      role="status"
    >
      <h2 className="mb-2 text-md font-semibold">{title}</h2>
      <p className={prose}>
        {children} The link disappears when you leave this page.
      </p>
      <pre
        tabIndex={0}
        className="mb-4 font-mono text-sm whitespace-pre-wrap wrap-anywhere select-all"
      >
        {url}
      </pre>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" touch onClick={() => void copy()}>
          Copy link
        </Button>
        <Button variant="ghost" touch onClick={onDismiss}>
          Dismiss link
        </Button>
        <span className="text-sm text-secondary">{copyStatus}</span>
      </div>
    </div>
  );
}
