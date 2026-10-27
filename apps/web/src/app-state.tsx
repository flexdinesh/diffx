import type { ApiCommitSummary } from "@diffx/api";
import {
  resolveCommit,
  stepCommit as stepCommitIn,
  type CommitView,
} from "./commit-view.ts";
import type { DiffMode } from "./diff-mode.ts";
import type { ReviewComment } from "./review-comments.ts";
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
  useSessionScope,
} from "./session-context.tsx";
import type { SyncSummary } from "./use-collection.ts";
import { useSidebarState } from "./sidebar-context.tsx";
import { useCommits } from "./use-commits.ts";
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
  /** The review scope; in a commit view, the scope it returns to. */
  mode: DiffMode;
  piped: boolean;
  scopeAvailable: boolean;
  requestedScope: string;
  changeMode: (mode: DiffMode) => void;
  commit: CommitView | null;
  /** Opens the commit view at the oldest commit. */
  openCommits: () => void;
  changeCommit: (commit: ApiCommitSummary) => void;
  /** Steps to the older (-1) or newer (1) commit; false at either end. */
  stepCommit: (delta: -1 | 1) => boolean;
  retryCommits: () => void;
  /** Syncs local checkouts when the process supports it, else reloads. */
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
const collapsedKey = (stream: string, view: string) =>
  `collapsed-files:${stream}:${view}`;
const noCommits: readonly ApiCommitSummary[] = [];

function WorkspaceProvider({ children }: { children: ReactNode }) {
  const session = useSession();
  const { id: contextId, capabilities } = session;
  const stream = streamKey(session);
  const { collect } = useCatalog();
  const refreshEnabled =
    collect !== null || capabilityEnabled(capabilities.diff.refresh);
  const commentsEnabled = capabilityEnabled(capabilities.review.comments);
  const scopes = capabilities.diff.scopes.values;
  const {
    mode,
    changeMode,
    scopeAvailable,
    requestedScope,
    commit: commitRequest,
    changeCommit: selectCommit,
    rememberCommit,
  } = useSessionScope();
  const captured = useCommits(contextId, session.commits !== undefined);
  const commitList = captured.status === "ready" ? captured.commits : noCommits;
  const resolved = useMemo(
    () =>
      commitRequest && captured.status === "ready"
        ? resolveCommit(captured.commits, commitRequest)
        : null,
    [commitRequest, captured],
  );
  // A remembered commit settles into the URL once resolved.
  const [commitNotice, setCommitNotice] = useState({ oid: "", text: "" });
  const settle =
    commitRequest?.kind === "saved" ? (resolved?.commit ?? null) : null;
  const settleNotice = resolved?.notice ?? "";
  useEffect(() => {
    if (!settle) return;
    setCommitNotice({ oid: settle.oid, text: settleNotice });
    selectCommit(settle);
  }, [settle, settleNotice, selectCommit]);
  const changeCommit = useCallback(
    (commit: ApiCommitSummary) => {
      setCommitNotice({ oid: "", text: "" });
      selectCommit(commit);
    },
    [selectCommit],
  );
  const commit = useMemo((): CommitView | null => {
    if (!commitRequest) return null;
    const summary = resolved?.commit ?? null;
    const status =
      captured.status === "error"
        ? "error"
        : captured.status === "loading" ||
            (commitRequest.kind === "saved" && summary)
          ? // A remembered commit is shown once it settles into the URL.
            "loading"
          : summary
            ? "ready"
            : "unavailable";
    return {
      oid: commitRequest.oid,
      summary: status === "ready" ? summary : null,
      status,
      detail: captured.status === "error" ? captured.detail : "",
      commits: commitList,
      omitted: captured.status === "ready" ? captured.omitted : 0,
      notice: commitNotice.oid === commitRequest.oid ? commitNotice.text : "",
    };
  }, [commitRequest, resolved, captured, commitList, commitNotice]);
  const shown = commit?.summary;
  useEffect(() => {
    if (shown) rememberCommit(shown);
  }, [shown, rememberCommit]);
  const commitOid = shown?.oid;
  // A branch reads in the order it was written.
  const openCommits = useCallback(() => {
    const [oldest] = commitList;
    if (oldest) changeCommit(oldest);
  }, [commitList, changeCommit]);
  const stepCommit = useCallback(
    (delta: -1 | 1) => {
      const next = commitOid
        ? stepCommitIn(commitList, commitOid, delta)
        : null;
      if (next) changeCommit(next);
      return next !== null;
    },
    [commitList, commitOid, changeCommit],
  );
  const viewingCommit = commit !== null;
  // Commit diffs exist only for captured non-merge commits.
  const commitTarget =
    commit?.summary && !commit.summary.merge ? commit.summary.oid : null;
  const viewKey = commit ? `commit:${commit.oid}` : mode;
  const [draft, setDraft] = useState<ReviewComment | null>(null);
  const [collapsed, setCollapsed] = useState(() =>
    sessionPaths(collapsedKey(stream, viewKey)),
  );
  const [collapsedView, setCollapsedView] = useState(viewKey);
  if (collapsedView !== viewKey) {
    setCollapsedView(viewKey);
    setCollapsed(sessionPaths(collapsedKey(stream, viewKey)));
  }
  useEffect(() => {
    saveSessionPaths(collapsedKey(stream, collapsedView), collapsed);
  }, [stream, collapsedView, collapsed]);
  const diff = useDiff(
    contextId,
    commitTarget
      ? { kind: "commit", oid: commitTarget }
      : { kind: "scope", mode },
    viewingCommit ? commitTarget !== null : scopeAvailable,
  );
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
    void collect().then((summary) => {
      if (summary) setFeedback(syncFeedback(summary));
    });
  }, [collect, reload, setFeedback]);
  // Review marks belong to scopes; a commit view has none.
  const reviewed = useReviewedFiles(
    contextId,
    viewingCommit ? null : repository,
    mode,
    setCollapsed,
  );
  const { show: showSidebar, closeMobile } = useSidebarState();
  const navigation = useFileNavigation({
    stream,
    repository,
    mode,
    scopes,
    busy: diff.busy,
    refreshEnabled,
    refresh,
    changeMode,
    viewingCommit,
    stepCommit,
    setCollapsed,
    isReviewed: reviewed.isReviewed,
    toggleReviewed: reviewed.toggleReviewed,
    pending: reviewed.pending,
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
      scopeAvailable,
      requestedScope,
      changeMode,
      commit,
      openCommits,
      changeCommit,
      stepCommit,
      retryCommits: captured.retry,
      refresh,
      refreshEnabled,
    }),
    [
      diff,
      repository,
      mode,
      piped,
      scopeAvailable,
      requestedScope,
      changeMode,
      commit,
      openCommits,
      changeCommit,
      stepCommit,
      captured.retry,
      refresh,
      refreshEnabled,
    ],
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

function syncFeedback({ checkouts, updated, failures }: SyncSummary) {
  const synced = `${checkouts} ${checkouts === 1 ? "checkout" : "checkouts"}`;
  if (!updated && !failures.length) return `No new changes in ${synced}.`;
  const failed = failures.length
    ? ` · ${failures.length} failed: ${failures.map((failure) => `${failure.path} (${failure.detail})`).join(", ")}`
    : "";
  return `Synced ${synced} · ${updated} updated${failed}`;
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
