import type { DiffMode, ReviewComment } from "@diffx/shared";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
  type Dispatch,
  type SetStateAction,
} from "react";
import { saveSessionPaths, sessionPaths } from "./preferences.ts";
import { streamKey } from "./project-nav.ts";
import {
  capabilityEnabled,
  useCatalog,
  useSession,
} from "./session-context.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { useDiff } from "./use-diff.ts";
import { useFileNavigation } from "./use-file-navigation.ts";
import { useReview } from "./use-review.ts";
import { useReviewedFiles } from "./use-reviewed-files.ts";

export function togglePath(paths: Set<string>, path: string) {
  const next = new Set(paths);
  if (next.has(path)) next.delete(path);
  else next.add(path);
  return next;
}

interface DiffSource {
  diff: ReturnType<typeof useDiff>;
  repository: ReturnType<typeof useDiff>["repository"];
  mode: DiffMode;
  piped: boolean;
  changeMode: (mode: DiffMode) => void;
  /** Collects the checkout again when the process supports it, else reloads. */
  refresh: () => void;
  refreshEnabled: boolean;
}
const SourceContext = createContext<DiffSource | null>(null);
const NavigationContext = createContext<ReturnType<
  typeof useFileNavigation
> | null>(null);
const ReviewContext = createContext<ReturnType<typeof useReview> | null>(null);
const ReviewedFilesContext = createContext<ReturnType<
  typeof useReviewedFiles
> | null>(null);
const DraftContext = createContext<{
  draft: ReviewComment | null;
  setDraft: Dispatch<SetStateAction<ReviewComment | null>>;
} | null>(null);
const CollapseContext = createContext<{
  collapsed: Set<string>;
  setCollapsed: Dispatch<SetStateAction<Set<string>>>;
} | null>(null);

// Collapse state belongs to the stream so it survives newer snapshots.
const collapsedKey = (stream: string, mode: DiffMode) =>
  `collapsed-files:${stream}:${mode}`;

function WorkspaceProvider({ children }: { children: ReactNode }) {
  const session = useSession();
  const { id: contextId, capabilities } = session;
  const stream = streamKey(session);
  const { collect } = useCatalog();
  const refreshEnabled =
    collect !== null || capabilityEnabled(capabilities.diff.refresh);
  const commentsEnabled = capabilityEnabled(capabilities.review.comments);
  const scopes = capabilities.diff.scopes.values;
  const [mode, setMode] = useState<DiffMode>("all");
  const [draft, setDraft] = useState<ReviewComment | null>(null);
  const [collapsed, setCollapsed] = useState(() =>
    sessionPaths(collapsedKey(stream, "all")),
  );
  useEffect(() => {
    saveSessionPaths(collapsedKey(stream, mode), collapsed);
  }, [stream, mode, collapsed]);
  const diff = useDiff(contextId, mode);
  const repository = diff.repository;
  const review = useReview(
    contextId,
    repository,
    draft,
    setDraft,
    commentsEnabled,
  );
  const { refresh: reload } = diff;
  const { setFeedback } = review;
  const refresh = useCallback(() => {
    if (!collect) return reload();
    void collect().then((result) => {
      if (result === "unchanged")
        setFeedback("No new changes since this snapshot.");
    });
  }, [collect, reload, setFeedback]);
  const reviewed = useReviewedFiles(contextId, repository, mode, setCollapsed);
  const { show: showSidebar, closeMobile } = useSidebarState();
  const changeMode = useCallback(
    (value: DiffMode) => {
      if (value === mode || !scopes.includes(value)) return;
      setMode(value);
      setCollapsed(sessionPaths(collapsedKey(stream, value)));
    },
    [stream, mode, scopes],
  );
  const navigation = useFileNavigation({
    stream,
    repository,
    mode,
    scopes,
    busy: diff.busy,
    refreshEnabled,
    refresh,
    changeMode,
    setCollapsed,
    setFeedback: review.setFeedback,
    showSidebar,
    closeMobile,
  });
  const piped = repository?.source === "stdin";
  useEffect(() => {
    document.title = `diffx · ${piped ? "Piped" : "Git diff"}`;
  }, [piped]);
  const source = useMemo(
    () => ({
      diff,
      repository,
      mode,
      piped,
      changeMode,
      refresh,
      refreshEnabled,
    }),
    [diff, repository, mode, piped, changeMode, refresh, refreshEnabled],
  );
  const draftState = useMemo(() => ({ draft, setDraft }), [draft]);
  const collapse = useMemo(() => ({ collapsed, setCollapsed }), [collapsed]);
  return (
    <SourceContext value={source}>
      <NavigationContext value={navigation}>
        <ReviewContext value={review}>
          <ReviewedFilesContext value={reviewed}>
            <DraftContext value={draftState}>
              <CollapseContext value={collapse}>{children}</CollapseContext>
            </DraftContext>
          </ReviewedFilesContext>
        </ReviewContext>
      </NavigationContext>
    </SourceContext>
  );
}

// Transient workspace ownership ends when the selected context changes.
export function AppProvider({ children }: { children: ReactNode }) {
  const { id } = useSession();
  return <WorkspaceProvider key={id}>{children}</WorkspaceProvider>;
}

export function useDiffSource() {
  const source = useContext(SourceContext);
  if (!source) throw new Error("Page components require AppProvider");
  return source;
}
export function useNavigation() {
  const navigation = useContext(NavigationContext);
  if (!navigation) throw new Error("Page components require AppProvider");
  return navigation;
}
export function useReviewState() {
  const review = useContext(ReviewContext);
  if (!review) throw new Error("Page components require AppProvider");
  return review;
}
export function useReviewedFilesState() {
  const reviewed = useContext(ReviewedFilesContext);
  if (!reviewed) throw new Error("Page components require AppProvider");
  return reviewed;
}
export function useDraft() {
  const draft = useContext(DraftContext);
  if (!draft) throw new Error("Page components require AppProvider");
  return draft;
}
export function useDiffCollapse() {
  const collapse = useContext(CollapseContext);
  if (!collapse) throw new Error("Page components require AppProvider");
  return collapse;
}
