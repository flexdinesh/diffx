import { api, errorDetail, type ApiContext } from "@diffx/api";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export type ContextCatalog = {
  contexts: ApiContext[];
  loaded: boolean;
  error: string;
  /** Applies a local removal and discards responses that started before it. */
  remove: (id: string) => void;
  retry: () => void;
};

// Ingestion events are hints: reload durable catalog state on each event,
// reconnect and visibility change, coalescing overlapping requests.
export function useContextCatalog(
  latest: boolean,
  enabled: boolean,
): ContextCatalog {
  const [contexts, setContexts] = useState<ApiContext[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const generation = useRef(0);
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    let busy = false;
    let pending = false;
    async function load() {
      if (document.hidden) return;
      if (busy) {
        pending = true;
        return;
      }
      busy = true;
      const started = generation.current;
      try {
        const entries: ApiContext[] = [];
        let cursor: string | undefined;
        do {
          const { data, error } = await api.GET("/api/v2/contexts", {
            params: {
              query: {
                limit: 100,
                ...(latest ? { latest: true } : {}),
                ...(cursor ? { cursor } : {}),
              },
            },
            signal: controller.signal,
          });
          if (!data)
            throw new Error(
              errorDetail(error, "Unable to list review contexts"),
            );
          entries.push(...data.contexts);
          cursor = data.nextCursor ?? undefined;
        } while (cursor && !controller.signal.aborted);
        if (controller.signal.aborted) return;
        if (started !== generation.current) {
          pending = true;
          return;
        }
        setContexts(entries);
        setLoaded(true);
        setError("");
      } catch (failure: unknown) {
        if (!controller.signal.aborted)
          setError(errorDetail(failure, "Unable to list review contexts"));
      } finally {
        busy = false;
        if (pending && !controller.signal.aborted) {
          pending = false;
          void load();
        }
      }
    }
    void load();
    const events = new EventSource("/api/v2/events");
    const update = () => void load();
    events.addEventListener("message", update);
    events.addEventListener("ingestion", update);
    events.addEventListener("open", update);
    // EventSource hides HTTP status; a normal API read resolves expired identity.
    events.addEventListener("error", update);
    const visible = () => {
      if (!document.hidden) void load();
    };
    document.addEventListener("visibilitychange", visible);
    return () => {
      controller.abort();
      events.close();
      document.removeEventListener("visibilitychange", visible);
    };
  }, [latest, enabled, attempt]);
  const remove = useCallback((id: string) => {
    generation.current++;
    setContexts((current) => current.filter((context) => context.id !== id));
  }, []);
  const retry = useCallback(() => {
    setError("");
    setAttempt((current) => current + 1);
  }, []);
  return useMemo(
    () => ({ contexts, loaded, error, remove, retry }),
    [contexts, loaded, error, remove, retry],
  );
}
