import {
  api,
  errorDetail,
  type ApiCollectionFailure,
  type ApiCollectionResult,
  type ApiContext,
} from "@diffx/api";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { LoaderCircleIcon } from "lucide-react";
import { AccountButton } from "./account.tsx";
import { ThemeButton } from "./ThemeButton.tsx";
import { Skeleton } from "@/components/ui/skeleton";
import { WorkspaceLoading } from "./WorkspaceLoading.tsx";
import { useIdentity, navigate } from "./identity-context.tsx";
import { Button } from "@/components/ui/button";
import { ContextIdentity } from "./ContextIdentity.tsx";
import { ProjectPicker } from "./ProjectPicker.tsx";
import { Navigation } from "./Navigation.tsx";
import {
  HeaderBrand,
  NewChangesButton,
  ProjectsToggle,
} from "./ProjectsNav.tsx";
import { latestChangedContext } from "./context-activity.ts";
import { unseenChangedHeads, type SeenStreams } from "./project-nav.ts";
import { useSeenStreams } from "./use-seen-streams.ts";
import { useSidebarState } from "./sidebar-context.tsx";
import { useCompactChrome } from "./use-compact-chrome.ts";
import {
  useContextCatalog,
  type ContextCatalog,
} from "./use-context-catalog.ts";

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
export interface SyncSummary {
  checkouts: number;
  /** Synced contexts that were not already current heads. */
  updated: number;
  failures: ApiCollectionFailure[];
}
const CatalogContext = createContext<Catalog | null>(null);
const SessionContext = createContext<ApiContext | null>(null);

export function capabilityEnabled(capability: { state: string }) {
  return capability.state === "enabled";
}

/**
 * `/contexts/{id}` pins one snapshot. `/streams/{id}` follows a stream's latest
 * head, resolved on load and navigation only. `/` resolves to a live stream.
 */
type Route =
  | { kind: "pinned"; id: string }
  | { kind: "live"; stream: string }
  | { kind: "root" };

function routeValue(prefix: string) {
  const match = new RegExp(`^/${prefix}/([^/]+)/?$`).exec(
    window.location.pathname,
  );
  if (!match?.[1]) return "";
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return "";
  }
}

function routeFromUrl(): Route {
  const id = routeValue("contexts");
  if (id) return { kind: "pinned", id };
  const stream = routeValue("streams");
  return stream ? { kind: "live", stream } : { kind: "root" };
}

export function contextPath(id: string) {
  return `/contexts/${encodeURIComponent(id)}`;
}

export function streamPath(head: ApiContext) {
  return head.streamId
    ? `/streams/${encodeURIComponent(head.streamId)}`
    : contextPath(head.id);
}

export function CurrentContext() {
  const catalog = useContext(CatalogContext);
  return catalog?.current ? (
    <ContextIdentity current={catalog.current} />
  ) : null;
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
  const [collecting, setCollecting] = useState(false);
  const [collectError, setCollectError] = useState("");
  const collectionRequest = useRef<Promise<ApiCollectionResult | null> | null>(
    null,
  );
  const selectedRef = useRef(selectedId);
  useEffect(() => {
    selectedRef.current = selectedId;
  }, [selectedId]);
  const statusRef = useRef<HTMLElement>(null);
  const restorePickerFocus = useRef(false);
  const compact = useCompactChrome();
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
  // Load-time navigation joins an active collection. Explicit Sync ignores
  // duplicate requests while the current collection is running.
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
  const headsRef = useRef(heads.contexts);
  useEffect(() => {
    headsRef.current = heads.contexts;
  }, [heads.contexts]);
  // Like arrivals in distributed mode, synced snapshots never replace the open
  // review; the newer-head notice and projects drawer announce them.
  const syncCheckouts = useCallback(async () => {
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
  }, [collectOnLoad, route, collectLatest]);
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
  return (
    <CatalogContext value={catalog}>
      <div className="app-shell">
        <div className="app-main">
          {ready ? (
            <SessionContext value={state.session}>{children}</SessionContext>
          ) : (
            <>
              <header className="topbar">
                {compact && <ProjectsToggle />}
                <HeaderBrand />
                <CurrentContext />
                {loading && !current && (
                  <Skeleton className="loading-identity" />
                )}
                <div className="header-actions">
                  <NewChangesButton />
                  {collecting && (
                    <Button
                      type="button"
                      id="sync"
                      variant="ghost"
                      size="icon"
                      aria-label="Sync all checkouts"
                      title="Syncing checkouts"
                      aria-busy="true"
                      disabled
                    >
                      <LoaderCircleIcon
                        className="size-(--icon-base)"
                        aria-hidden="true"
                      />
                    </Button>
                  )}
                  {compact && <AccountButton />}
                  <ThemeButton />
                </div>
              </header>
              <div className="workbench">
                <Navigation loading={loading} />
                <main
                  className={`session-status${loading ? " is-loading" : ""}`}
                  role="status"
                  aria-busy={loading}
                  ref={statusRef}
                  tabIndex={-1}
                >
                  {loading ? (
                    <WorkspaceLoading collecting={collectOnLoad} />
                  ) : (
                    <>
                      <h1>
                        {statusError
                          ? collectError
                            ? "Cannot sync changes"
                            : "Cannot load diffx"
                          : missing
                            ? "No current snapshot"
                            : empty
                              ? heads.contexts.length
                                ? "No changes to review"
                                : "No review contexts yet"
                              : "Loading diffx"}
                      </h1>
                      <p>
                        {statusError ||
                          (missing
                            ? "This stream's snapshots expired or were deleted. Pick current work from Repos."
                            : empty
                              ? remote
                                ? "Create a collector token in settings, then run diffx sync to publish your changes."
                                : "Run diffx to review a checkout, or diffx sync to publish remotely."
                              : "Reading review context…")}
                      </p>
                      {empty && remote && (
                        <Button
                          variant="outline"
                          onClick={() => navigate("/settings")}
                        >
                          Create a collector token
                        </Button>
                      )}
                      {statusError && (
                        <Button
                          type="button"
                          variant="outline"
                          onClick={() => {
                            setState({ status: "loading" });
                            if (collectError) {
                              collected.current = null;
                              setCollectError("");
                              setMissing(false);
                              setCollectionResult(null);
                            }
                            reloadHeads();
                            setAttempt((current) => current + 1);
                          }}
                        >
                          Retry
                        </Button>
                      )}
                    </>
                  )}
                </main>
              </div>
            </>
          )}
        </div>
      </div>
      <ProjectPicker
        contexts={history.loaded ? history.contexts : heads.contexts}
        selectedId={selectedId}
        select={select}
        open={pickerOpen}
        onOpenChange={setPickerOpen}
      />
    </CatalogContext>
  );
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
