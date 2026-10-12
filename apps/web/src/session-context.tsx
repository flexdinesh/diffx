import { api, errorDetail, type ApiContext } from "@diffx/api";
import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type RefObject,
} from "react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { ProjectPicker, ProjectPickerTrigger } from "./ProjectPicker.tsx";
import { ProjectsNav, ProjectsToggle } from "./ProjectsNav.tsx";
import { contextHasChanges } from "./project-picker.ts";
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
  selectedId: string;
  select: (id: string, options?: { restoreFocus?: boolean }) => void;
  deleteContext: (id: string) => Promise<void>;
  pickerOpen: boolean;
  openPicker: () => void;
  triggerRef: RefObject<HTMLButtonElement | null>;
}
const CatalogContext = createContext<Catalog | null>(null);
const SessionContext = createContext<ApiContext | null>(null);

export function capabilityEnabled(capability: { state: string }) {
  return capability.state === "enabled";
}

function contextFromUrl() {
  const match = /^\/contexts\/([^/]+)\/?$/.exec(window.location.pathname);
  if (!match?.[1]) return "";
  try {
    return decodeURIComponent(match[1]);
  } catch {
    return "";
  }
}

export function contextPath(id: string) {
  return `/contexts/${encodeURIComponent(id)}`;
}

export function ContextSwitcher() {
  const catalog = useContext(CatalogContext);
  if (!catalog || (!catalog.heads.contexts.length && !catalog.current))
    return null;
  return (
    <ProjectPickerTrigger
      current={catalog.current}
      open={catalog.pickerOpen}
      onOpen={catalog.openPicker}
      triggerRef={catalog.triggerRef}
    />
  );
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const [selectedId, setSelectedId] = useState(contextFromUrl);
  const [state, setState] = useState<LoadState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [pickerOpen, setPickerOpen] = useState(false);
  // Navigation needs only current work; the picker's history loads on demand.
  const heads = useContextCatalog(true, true);
  const history = useContextCatalog(false, pickerOpen);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const statusRef = useRef<HTMLElement>(null);
  const restorePickerFocus = useRef(false);
  const openPicker = useCallback(() => setPickerOpen(true), []);
  const select = useCallback(
    (id: string, options?: { restoreFocus?: boolean }) => {
      if (id === contextFromUrl()) {
        window.history.replaceState(null, "", contextPath(id));
        return;
      }
      restorePickerFocus.current = options?.restoreFocus ?? true;
      window.history.pushState(null, "", contextPath(id));
      setSelectedId(id);
      setState({ status: "loading" });
    },
    [],
  );
  const { remove: removeHead, retry: reloadHeads } = heads;
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
      if (contextFromUrl() === id) {
        const next = heads.contexts.find(
          (context) => context.id !== id && contextHasChanges(context),
        );
        restorePickerFocus.current = true;
        window.history.replaceState(
          null,
          "",
          next ? contextPath(next.id) : "/",
        );
        setSelectedId(next?.id ?? "");
        setState({ status: "loading" });
      }
      reloadHeads();
    },
    [heads.contexts, removeHead, removeHistory, reloadHeads],
  );
  useEffect(() => {
    const handleNavigation = () => {
      setSelectedId(contextFromUrl());
      setState({ status: "loading" });
    };
    window.addEventListener("popstate", handleNavigation);
    return () => window.removeEventListener("popstate", handleNavigation);
  }, []);
  // The root URL opens the most recently submitted current work.
  useEffect(() => {
    if (contextFromUrl()) return;
    const first = heads.contexts.find(contextHasChanges);
    if (!first) return;
    window.history.replaceState(null, "", contextPath(first.id));
    setSelectedId(first.id);
  }, [heads.contexts]);
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
  const catalog = useMemo(
    () => ({
      heads,
      current,
      selectedId,
      select,
      deleteContext,
      pickerOpen,
      openPicker,
      triggerRef,
    }),
    [heads, current, selectedId, select, deleteContext, pickerOpen, openPicker],
  );
  const empty = heads.loaded && !selectedId;
  useEffect(() => {
    if (!restorePickerFocus.current || (!empty && state.status === "loading"))
      return;
    restorePickerFocus.current = false;
    if (empty) statusRef.current?.focus();
    else triggerRef.current?.focus();
  }, [state, empty]);
  return (
    <CatalogContext value={catalog}>
      <div className="app-shell">
        <ProjectsNav />
        <div className="app-main">
          {ready ? (
            <SessionContext value={state.session}>{children}</SessionContext>
          ) : (
            <>
              <header className="topbar">
                <ProjectsToggle />
                <a className="brand" href="/" aria-label="diffx home">
                  diffx
                </a>
                <Separator className="header-divider" orientation="vertical" />
                <ContextSwitcher />
              </header>
              <main
                className="session-status"
                role="status"
                ref={statusRef}
                tabIndex={-1}
              >
                <h1>
                  {empty
                    ? heads.contexts.length
                      ? "No changes to review"
                      : "No review contexts yet"
                    : state.status === "error" || heads.error
                      ? "Cannot load diffx"
                      : "Loading diffx"}
                </h1>
                <p>
                  {empty
                    ? "Run diffx to review a checkout, or diffx sync to publish remotely."
                    : state.status === "error"
                      ? state.detail
                      : heads.error || "Reading review context…"}
                </p>
                {(state.status === "error" || heads.error) && (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setState({ status: "loading" });
                      reloadHeads();
                      setAttempt((current) => current + 1);
                    }}
                  >
                    Retry
                  </Button>
                )}
              </main>
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
        triggerRef={triggerRef}
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
