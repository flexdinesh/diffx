import { api, errorDetail, type ApiContext } from "@diffx/api";
import {
  createContext,
  type ReactNode,
  type RefObject,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useIdentity } from "./identity-context.tsx";
import { latestChangedContext } from "./context-activity.ts";
import { unseenChangedHeads, type SeenStreams } from "./project-nav.ts";
import { useSeenStreams } from "./use-seen-streams.ts";
import { useSidebarState } from "./sidebar-context.tsx";
import {
  useContextCatalog,
  type ContextCatalog,
} from "./use-context-catalog.ts";
import {
  contextPath,
  routeFromUrl,
  streamPath,
  type Route,
} from "./review-route.ts";
import { useCollection, type SyncSummary } from "./use-collection.ts";

type LoadState =
  | { status: "loading" }
  | { status: "error"; detail: string }
  | { status: "ready"; session: ApiContext };

interface Catalog {
  /** Non-stale snapshots: each stream's head plus piped diffs. */
  heads: ContextCatalog;
  /** The selected review once loaded, otherwise its catalog entry if known. */
  current: ApiContext | undefined;
  seen: SeenStreams | null;
  unreadHeads: ApiContext[];
  selectedId: string;
  /** Pins one snapshot. */
  select: (id: string, options?: { restoreFocus?: boolean }) => void;
  /**
   * Follows a stream's latest head; piped snapshots stay pinned. Opens the Diff
   * view unless navigation is preserved.
   */
  openStream: (
    head: ApiContext,
    options?: { restoreFocus?: boolean; preserveNavigation?: boolean },
  ) => void;
  deleteContext: (id: string) => Promise<void>;
  pickerOpen: boolean;
  openPicker: () => void;
  /**
   * Syncs every checkout of the local process without replacing the open
   * review; null when unsupported. Resolves null when the sync failed.
   */
  collect: (() => Promise<SyncSummary | null>) | null;
  collecting: boolean;
  collectError: string;
}
/** Loading, empty and failure state for the shell shown until a review is ready. */
export interface SessionShellState {
  ready: boolean;
  loading: boolean;
  empty: boolean;
  missing: boolean;
  statusError: string;
  collectError: string;
  collectOnLoad: boolean;
  collecting: boolean;
  remote: boolean;
  statusRef: RefObject<HTMLElement | null>;
  retry: () => void;
  history: ContextCatalog;
  setPickerOpen: (open: boolean) => void;
}
const CatalogContext = createContext<Catalog | null>(null);
const SessionContext = createContext<ApiContext | null>(null);
const SessionShellContext = createContext<SessionShellState | null>(null);

export function capabilityEnabled(capability: { state: string }) {
  return capability.state === "enabled";
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const { state: identity } = useIdentity();
  const remote =
    identity.status === "ready" && identity.bootstrap.mode === "remote";
  const collectEnabled =
    identity.status === "ready" &&
    identity.bootstrap.mode === "local" &&
    identity.bootstrap.collect;
  const [route, setRoute] = useState(routeFromUrl);
  const [selectedId, setSelectedId] = useState(() =>
    route.kind === "pinned" ? route.id : "",
  );
  // A live route without a current head (expired, deleted, or unknown).
  const [missing, setMissing] = useState(false);
  const [state, setState] = useState<LoadState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [pickerOpen, setPickerOpen] = useState(false);
  // Navigation needs only current work; the picker's history loads on demand.
  const heads = useContextCatalog(true, true);
  const history = useContextCatalog(false, pickerOpen);
  const selectedRef = useRef(selectedId);
  useEffect(() => {
    selectedRef.current = selectedId;
  }, [selectedId]);
  const statusRef = useRef<HTMLElement>(null);
  const restorePickerFocus = useRef(false);
  const { openDiff } = useSidebarState();
  const openPicker = useCallback(() => setPickerOpen(true), []);
  const show = useCallback(
    (
      next: Route,
      path: string,
      id: string,
      options?: {
        restoreFocus?: boolean;
        replace?: boolean;
        preserveNavigation?: boolean;
      },
    ) => {
      if (!options?.preserveNavigation) openDiff();
      if (options?.replace || path === window.location.pathname)
        window.history.replaceState(null, "", path);
      else window.history.pushState(null, "", path);
      setRoute(next);
      setMissing(false);
      if (id === selectedRef.current) {
        if (options?.restoreFocus !== false)
          requestAnimationFrame(() =>
            document
              .querySelector<HTMLElement>("#diff-view, #sidebar-toggle")
              ?.focus(),
          );
        return;
      }
      restorePickerFocus.current = options?.restoreFocus ?? true;
      setSelectedId(id);
      setState({ status: "loading" });
    },
    [openDiff],
  );
  const select = useCallback(
    (id: string, options?: { restoreFocus?: boolean }) =>
      show({ kind: "pinned", id }, contextPath(id), id, options),
    [show],
  );
  const openStream = useCallback(
    (
      head: ApiContext,
      options?: {
        restoreFocus?: boolean;
        replace?: boolean;
        preserveNavigation?: boolean;
      },
    ) =>
      show(
        head.streamId
          ? { kind: "live", stream: head.streamId }
          : { kind: "pinned", id: head.id },
        streamPath(head),
        head.id,
        options,
      ),
    [show],
  );
  const { remove: removeHead, retry: reloadHeads } = heads;
  const {
    collectLatest,
    syncCheckouts,
    collecting,
    collectError,
    setCollectError,
  } = useCollection(heads.contexts, reloadHeads);
  const collect = collectEnabled ? syncCheckouts : null;
  const { remove: removeHistory } = history;
  const deleteContext = useCallback(
    async (id: string) => {
      const { error, response } = await api.DELETE(
        "/api/v2/contexts/{contextId}",
        {
          params: { path: { contextId: id } },
        },
      );
      if (!response.ok && response.status !== 404)
        throw new Error(
          errorDetail(error, "Unable to delete review context. Try again."),
        );
      removeHead(id);
      removeHistory(id);
      if (selectedRef.current === id) {
        const next = latestChangedContext(
          heads.contexts.filter((context) => context.id !== id),
        );
        if (next) openStream(next, { replace: true });
        else {
          restorePickerFocus.current = true;
          window.history.replaceState(null, "", "/");
          setRoute({ kind: "root" });
          setSelectedId("");
          setState({ status: "loading" });
        }
      }
      reloadHeads();
    },
    [heads.contexts, removeHead, removeHistory, reloadHeads, openStream],
  );
  useEffect(() => {
    const handleNavigation = () => {
      const next = routeFromUrl();
      setRoute(next);
      setMissing(false);
      setSelectedId(next.kind === "pinned" ? next.id : "");
      setState({ status: "loading" });
    };
    window.addEventListener("popstate", handleNavigation);
    return () => window.removeEventListener("popstate", handleNavigation);
  }, []);
  // Live URLs open available catalog data while local collection continues; the
  // root waits for the collection's default. Once selected, a snapshot stays
  // open; collection only announces newer heads.
  const collected = useRef<Route | null>(null);
  const [collectionResult, setCollectionResult] = useState<{
    route: Route;
    contexts: ApiContext[];
    defaultContextId: string;
  } | null>(null);
  const resolving = route.kind !== "pinned" && !selectedId && !missing;
  const collectionFinished = collectionResult?.route === route;
  const collectOnLoad = resolving && collectEnabled && !collectionFinished;
  useEffect(() => {
    if (!collectOnLoad || collected.current === route) return;
    // Effect replay must not start another collection. Completion only publishes
    // a result; route resolution below cannot replace a user-selected review.
    collected.current = route;
    void collectLatest(true).then((result) => {
      if (result?.failures.length)
        setCollectError(
          `Unable to sync ${result.failures.length} ${result.failures.length === 1 ? "checkout" : "checkouts"}: ${result.failures.map((failure) => `${failure.path} (${failure.detail})`).join(", ")}`,
        );
      setCollectionResult({
        route,
        contexts: result?.contexts ?? [],
        defaultContextId: result?.defaultContextId ?? "",
      });
    });
  }, [collectOnLoad, route, collectLatest, setCollectError]);
  useEffect(() => {
    if (!resolving) return;
    const findHead = (contexts: ApiContext[]) =>
      route.kind === "live"
        ? contexts.find((context) => context.streamId === route.stream)
        : latestChangedContext(contexts);
    const collectedHead =
      collectionResult?.route !== route
        ? undefined
        : route.kind === "live"
          ? findHead(collectionResult.contexts)
          : findHead(
              collectionResult.contexts.filter(
                (context) => context.id === collectionResult.defaultContextId,
              ),
            );
    // Only the finished collection names the root's default, such as the
    // launched checkout; earlier heads are whichever checkouts finished first.
    const head =
      collectedHead ??
      (route.kind === "root" && collectOnLoad
        ? undefined
        : findHead(heads.contexts));
    if (head)
      openStream(head, {
        replace: true,
        restoreFocus: false,
        preserveNavigation: true,
      });
    else if (route.kind === "live" && heads.loaded && !collectOnLoad)
      setMissing(true);
  }, [
    resolving,
    heads.loaded,
    heads.contexts,
    collectOnLoad,
    collectionResult,
    route,
    openStream,
  ]);
  useEffect(() => {
    if (!selectedId) return;
    const controller = new AbortController();
    setState({ status: "loading" });
    void api
      .GET("/api/v2/contexts/{contextId}", {
        params: { path: { contextId: selectedId } },
        signal: controller.signal,
      })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (data) setState({ status: "ready", session: data });
        else
          setState({
            status: "error",
            detail: errorDetail(error, "Unable to load review context"),
          });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({
            status: "error",
            detail: errorDetail(error, "Unable to load review context"),
          });
      });
    return () => controller.abort();
  }, [selectedId, attempt]);
  const ready = state.status === "ready" && state.session.id === selectedId;
  const current = ready
    ? state.session
    : heads.contexts.find((context) => context.id === selectedId);
  const seen = useSeenStreams(
    identity.status === "ready" ? (identity.bootstrap.user?.id ?? null) : null,
    heads.contexts,
    heads.loaded,
    ready ? current : undefined,
  );
  const unreadHeads = useMemo(
    () => unseenChangedHeads(heads.contexts, seen),
    [heads.contexts, seen],
  );
  const catalog = useMemo(
    () => ({
      heads,
      current,
      seen,
      unreadHeads,
      selectedId,
      select,
      openStream,
      deleteContext,
      pickerOpen,
      openPicker,
      collect,
      collecting,
      collectError,
    }),
    [
      heads,
      current,
      seen,
      unreadHeads,
      selectedId,
      select,
      openStream,
      deleteContext,
      pickerOpen,
      openPicker,
      collect,
      collecting,
      collectError,
    ],
  );
  // The root URL keeps waiting for changed work while showing the empty state.
  const empty =
    heads.loaded && !selectedId && route.kind === "root" && !collectOnLoad;
  const statusError =
    state.status === "error" ? state.detail : heads.error || collectError;
  const loading = !statusError && !empty && !missing;
  useEffect(() => {
    if (!restorePickerFocus.current || (!empty && state.status === "loading"))
      return;
    restorePickerFocus.current = false;
    if (empty) statusRef.current?.focus();
    else {
      const selected = document.querySelector<HTMLElement>(
        '#projects-nav [aria-current="page"]',
      );
      const target = selected?.getClientRects().length
        ? selected
        : document.querySelector<HTMLElement>("#diff-view, #sidebar-toggle");
      target?.focus();
    }
  }, [state, empty]);
  const retry = useCallback(() => {
    setState({ status: "loading" });
    if (collectError) {
      collected.current = null;
      setCollectError("");
      setMissing(false);
      setCollectionResult(null);
    }
    reloadHeads();
    setAttempt((current) => current + 1);
  }, [collectError, setCollectError, reloadHeads]);
  const shell = useMemo(
    () => ({
      ready,
      loading,
      empty,
      missing,
      statusError,
      collectError,
      collectOnLoad,
      collecting,
      remote,
      statusRef,
      retry,
      history,
      setPickerOpen,
    }),
    [
      ready,
      loading,
      empty,
      missing,
      statusError,
      collectError,
      collectOnLoad,
      collecting,
      remote,
      retry,
      history,
    ],
  );
  return (
    <CatalogContext value={catalog}>
      <SessionShellContext value={shell}>
        <SessionContext value={ready ? state.session : null}>
          {children}
        </SessionContext>
      </SessionShellContext>
    </CatalogContext>
  );
}

export function useSessionShell() {
  const shell = useContext(SessionShellContext);
  if (!shell) throw new Error("Session shell requires SessionProvider");
  return shell;
}

export function useCatalog() {
  const catalog = useContext(CatalogContext);
  if (!catalog) throw new Error("Catalog consumers require SessionProvider");
  return catalog;
}

export function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error("Page components require SessionProvider");
  return session;
}

export function useDeleteContext() {
  const catalog = useContext(CatalogContext);
  if (!catalog) throw new Error("Context deletion requires SessionProvider");
  return catalog.deleteContext;
}
