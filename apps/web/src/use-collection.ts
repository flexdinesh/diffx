import {
  api,
  errorDetail,
  type ApiCollectionFailure,
  type ApiCollectionResult,
  type ApiContext,
} from "@diffx/api";
import { useCallback, useEffect, useRef, useState } from "react";

export interface SyncSummary {
  checkouts: number;
  /** Synced contexts that were not already current heads. */
  updated: number;
  failures: ApiCollectionFailure[];
}

/**
 * Owns local checkout collection requests. Load-time navigation joins an
 * active collection; explicit Sync ignores duplicates while one is running.
 */
export function useCollection(heads: ApiContext[], reloadHeads: () => void) {
  const [collecting, setCollecting] = useState(false);
  const [collectError, setCollectError] = useState("");
  const collectionRequest = useRef<Promise<ApiCollectionResult | null> | null>(
    null,
  );
  const collectLatest = useCallback(
    (initial = false) => {
      if (collectionRequest.current)
        return initial ? collectionRequest.current : Promise.resolve(null);
      setCollecting(true);
      setCollectError("");
      const request = (async () => {
        try {
          const { data, error } = await api.POST("/api/v2/collections", {
            params: { query: { initial } },
          });
          if (!data)
            throw new Error(errorDetail(error, "Unable to sync changes"));
          reloadHeads();
          return data;
        } catch (error) {
          setCollectError(errorDetail(error, "Unable to sync changes"));
          return null;
        } finally {
          collectionRequest.current = null;
          setCollecting(false);
        }
      })();
      collectionRequest.current = request;
      return request;
    },
    [reloadHeads],
  );
  const headsRef = useRef(heads);
  useEffect(() => {
    headsRef.current = heads;
  }, [heads]);
  // Like arrivals in distributed mode, synced snapshots never replace the open
  // review; the newer-head notice and projects drawer announce them.
  const syncCheckouts = useCallback(async (): Promise<SyncSummary | null> => {
    const current = new Set(headsRef.current.map((context) => context.id));
    const result = await collectLatest();
    if (!result) return null;
    return {
      checkouts: result.contexts.length + result.failures.length,
      updated: result.contexts.filter((context) => !current.has(context.id))
        .length,
      failures: result.failures,
    };
  }, [collectLatest]);
  return {
    collectLatest,
    syncCheckouts,
    collecting,
    collectError,
    setCollectError,
  };
}
