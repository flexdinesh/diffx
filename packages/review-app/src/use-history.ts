import { api, errorDetail } from "@diffx/api";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  historyPath,
  historyQuery,
  historyRoute,
  type HistoryActivity,
  type HistoryData,
  type HistoryOverviewData,
  type HistoryPullRequest,
  type HistoryQuery,
  type HistoryStateFilter,
} from "./history.ts";

/** History's URL state; changes push a history entry and Back restores them. */
export function useHistoryQuery() {
  const [query, setQueryState] = useState(() =>
    historyQuery(window.location.search),
  );
  useEffect(() => {
    const changed = () => {
      if (window.location.pathname === historyRoute)
        setQueryState(historyQuery(window.location.search));
    };
    window.addEventListener("popstate", changed);
    return () => window.removeEventListener("popstate", changed);
  }, []);
  const setQuery = useCallback((next: HistoryQuery) => {
    window.history.pushState(null, "", historyPath(next));
    setQueryState(next);
  }, []);
  return [query, setQuery] as const;
}

export type HistoryLoad<T> =
  | { status: "loading" }
  | { status: "failed"; message: string }
  | { status: "ready"; data: T };

const failure = "Unable to load history. Try again.";

type Settled<T> = { key: string; load: HistoryLoad<T> };

/**
 * Runs `read` for `key` while enabled, again on Retry and whenever the key
 * changes; a newer read aborts the one in flight so stale answers never land.
 * `last` keeps the latest answer, with its key, across later reads.
 */
function useHistoryRead<T>(
  key: string,
  read: (signal: AbortSignal) => Promise<T>,
  enabled: boolean,
) {
  const [settled, setSettled] = useState<Settled<T> | null>(null);
  const [last, setLast] = useState<{ key: string; data: T } | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    setSettled({ key, load: { status: "loading" } });
    read(controller.signal)
      .then((data) => {
        if (controller.signal.aborted) return;
        setSettled({ key, load: { status: "ready", data } });
        setLast({ key, data });
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted)
          setSettled({
            key,
            load: { status: "failed", message: errorDetail(cause, failure) },
          });
      });
    return () => controller.abort();
  }, [key, read, enabled, attempt]);
  const retry = useCallback(() => setAttempt((value) => value + 1), []);
  const load: HistoryLoad<T> =
    settled?.key === key ? settled.load : { status: "loading" };
  return { load, retry, last };
}

/** The overview's aggregates over the bins `edges` define. */
export function useHistoryOverview(edges: readonly number[], enabled: boolean) {
  const key = edges.join(",");
  const read = useCallback(
    async (signal: AbortSignal) => {
      const { data, error } = await api.GET("/api/v2/github/history/overview", {
        params: { query: { edges: key.split(",").map(Number) } },
        signal,
      });
      if (!data) throw new Error(errorDetail(error, failure));
      return data;
    },
    [key],
  );
  const { load, retry } = useHistoryRead<HistoryOverviewData>(
    key,
    read,
    enabled,
  );
  return { load, retry };
}

export type HistoryListFilters = {
  repo: string;
  activity: HistoryActivity;
  state: HistoryStateFilter;
};

const pageSize = 200;

type Pages = {
  /** The first page these follow; a new first page discards them. */
  owner: HistoryData;
  pulls: HistoryPullRequest[];
  cursor: string | null;
  status: "idle" | "loading" | "failed";
};

function firstPages(data: HistoryData): Pages {
  return { owner: data, pulls: [], cursor: data.nextCursor, status: "idle" };
}

/**
 * The window's filtered pull requests, a page at a time: the first page
 * carries the window's totals (kept as `totals` while filters change),
 * `loadMore` appends the next page, and `drain` reads on until `limit` rows
 * or the last page. A new window or filter starts over and aborts pages in
 * flight.
 */
export function useHistory(
  span: { since: number; until: number },
  filters: HistoryListFilters,
  enabled: boolean,
) {
  const { since, until } = span;
  const { repo, activity, state } = filters;
  const page = useCallback(
    async (cursor: string | null, signal: AbortSignal) => {
      const { data, error } = await api.GET("/api/v2/github/history", {
        params: {
          query: {
            since,
            until,
            limit: pageSize,
            ...(cursor === null ? {} : { cursor }),
            ...(repo ? { repo } : {}),
            ...(activity ? { activity } : {}),
            ...(state ? { state } : {}),
          },
        },
        signal,
      });
      if (!data) throw new Error(errorDetail(error, failure));
      return data;
    },
    [since, until, repo, activity, state],
  );
  const first = useCallback(
    (signal: AbortSignal) => page(null, signal),
    [page],
  );
  const windowKey = `${since}:${until}`;
  const { load, retry, last } = useHistoryRead<HistoryData>(
    `${windowKey}|${repo}|${activity}|${state}`,
    first,
    enabled,
  );
  const data = load.status === "ready" ? load.data : null;
  // Totals cover the window whatever the filters, so they outlast a filter
  // change's read.
  const totals =
    last && last.key.split("|")[0] === windowKey ? last.data : null;

  const [pagesState, setPagesState] = useState<Pages | null>(null);
  const pages = useRef<Pages | null>(null);
  const inflight = useRef<Promise<Pages | null> | null>(null);
  const controller = useRef<AbortController | null>(null);
  const setPages = useCallback((next: Pages) => {
    pages.current = next;
    setPagesState(next);
  }, []);
  // A new first page (or leaving the list) abandons the pages after it.
  useEffect(
    () => () => {
      controller.current?.abort();
      controller.current = null;
      inflight.current = null;
    },
    [data],
  );

  const current = data
    ? pagesState?.owner === data
      ? pagesState
      : firstPages(data)
    : null;

  const next = useCallback((): Promise<Pages | null> => {
    if (!data) return Promise.resolve(null);
    if (inflight.current) return inflight.current;
    const base =
      pages.current?.owner === data ? pages.current : firstPages(data);
    const cursor = base.cursor;
    if (cursor === null) return Promise.resolve(base);
    const abort = new AbortController();
    controller.current = abort;
    setPages({ ...base, status: "loading" });
    const read = page(cursor, abort.signal)
      .then((more): Pages | null => {
        if (abort.signal.aborted) return null;
        const after: Pages = {
          owner: data,
          pulls: [...base.pulls, ...more.pullRequests],
          cursor: more.nextCursor,
          status: "idle",
        };
        setPages(after);
        return after;
      })
      .catch(() => {
        if (!abort.signal.aborted) setPages({ ...base, status: "failed" });
        return null;
      })
      .finally(() => {
        if (inflight.current === read) inflight.current = null;
      });
    inflight.current = read;
    return read;
  }, [data, page, setPages]);

  const loadMore = useCallback(() => {
    void next();
  }, [next]);

  /** Reads on to `limit` rows; null when a page fails or the list changes. */
  const drain = useCallback(
    async (limit: number) => {
      if (!data) return null;
      let latest: Pages | null =
        pages.current?.owner === data ? pages.current : firstPages(data);
      while (
        latest.cursor !== null &&
        data.pullRequests.length + latest.pulls.length < limit
      ) {
        latest = await next();
        if (!latest) return null;
      }
      const all = [...data.pullRequests, ...latest.pulls];
      return {
        pulls: all.slice(0, limit),
        capped: all.length > limit || latest.cursor !== null,
      };
    },
    [data, next],
  );

  return {
    load,
    retry,
    totals,
    pulls: data && current ? [...data.pullRequests, ...current.pulls] : [],
    hasMore: current ? current.cursor !== null : false,
    more: current ? current.status : "idle",
    loadMore,
    drain,
  };
}
