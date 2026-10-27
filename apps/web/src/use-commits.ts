import { api, errorDetail, type ApiCommitList } from "@diffx/api";
import { useCallback, useEffect, useMemo, useState } from "react";

type CommitsState =
  | { status: "loading" }
  | { status: "error"; detail: string }
  | ({ status: "ready" } & ApiCommitList);

const noCommits: CommitsState = { status: "ready", commits: [], omitted: 0 };
const loading: CommitsState = { status: "loading" };

/** The snapshot's captured commits, oldest first. Observations are immutable. */
export function useCommits(contextId: string, captured: boolean) {
  const [state, setState] = useState<{ key: string; value: CommitsState }>({
    key: "",
    value: loading,
  });
  const [attempt, setAttempt] = useState(0);
  const key = `${contextId}:${attempt}`;
  useEffect(() => {
    if (!captured) return;
    const controller = new AbortController();
    void api
      .GET("/api/v2/contexts/{contextId}/commits", {
        params: { path: { contextId } },
        signal: controller.signal,
      })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        setState({
          key,
          value: data
            ? { status: "ready", ...data }
            : {
                status: "error",
                detail: errorDetail(error, "Unable to load commits"),
              },
        });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({
            key,
            value: {
              status: "error",
              detail: errorDetail(error, "Unable to load commits"),
            },
          });
      });
    return () => controller.abort();
  }, [contextId, captured, key]);
  const retry = useCallback(() => setAttempt((current) => current + 1), []);
  const value: CommitsState = !captured
    ? noCommits
    : state.key === key
      ? state.value
      : loading;
  return useMemo(() => ({ ...value, retry }), [value, retry]);
}
