import type { CodeViewItem } from "@pierre/diffs";
import type { CodeViewHandle } from "@pierre/diffs/react";
import type {
  ApiChangedFile as ChangedFile,
  ApiRepositoryDiff as RepositoryDiff,
} from "@diffx/api";
import type { DiffMode } from "./diff-mode.ts";
import type { ReviewComment } from "./review-comments.ts";
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { ancestorPaths, filesInTreeOrder } from "./file-tree.ts";
import {
  changeStops,
  stepChangeStop,
  stepInViewport,
  type ChangeStop,
  type LineSpan,
  type StopKey,
  type StopRect,
} from "./hunk-navigation.ts";
import {
  saveSessionFlag,
  saveSessionPaths,
  sessionFlag,
  sessionPaths,
} from "./preferences.ts";
import {
  commentInFile,
  placedInline,
  placedOutdated,
  placementFor,
  type CommentAnnotation,
} from "./review-model.ts";

// Inline comments open at their placed lines; outdated ones at their file's top.
function commentTarget(
  comment: ReviewComment,
  repository: RepositoryDiff | null,
) {
  const file = repository?.files.find((entry) => commentInFile(comment, entry));
  if (!file) return null;
  const placement = placementFor(comment, repository);
  if (placedInline(placement.applicability))
    return comment.target === "file"
      ? { path: file.path, range: null }
      : { path: file.path, range: placement };
  return placedOutdated(placement.applicability)
    ? { path: file.path, range: null }
    : null;
}

interface NavigationOptions {
  /** Persisted tree state belongs to the viewed stream. */
  stream: string;
  repository: RepositoryDiff | null;
  mode: DiffMode;
  scopes: readonly DiffMode[];
  busy: boolean;
  refreshEnabled: boolean;
  refresh: () => void;
  changeMode: (mode: DiffMode) => void;
  /** A read-only commit view replaces the scope's diff. */
  viewingCommit: boolean;
  /** Whether reviewed-file marks apply (Alt+M); see `DiffSource.marking`. */
  marking: boolean;
  stepCommit: (delta: -1 | 1) => boolean;
  /** Loaded previews by path; change navigation reads their hunks. */
  items: ReadonlyMap<string, CodeViewItem<CommentAnnotation>>;
  /** Files after the last loaded preview, waiting for Load more. */
  unloaded: number;
  collapsed: ReadonlySet<string>;
  setCollapsed: Dispatch<SetStateAction<Set<string>>>;
  isReviewed: (file: ChangedFile) => boolean;
  /** Resolves whether the toggle was applied. */
  toggleReviewed: (file: ChangedFile) => Promise<boolean>;
  /** File ids, or "*" for a reset, with a reviewed-mark write in flight. */
  pending: ReadonlySet<string>;
  setFeedback: (message: string) => void;
  showSidebar: () => void;
  closeMobile: () => void;
}

export function useFileNavigation({
  stream,
  repository,
  mode,
  scopes,
  busy,
  refreshEnabled,
  refresh,
  changeMode,
  viewingCommit,
  marking,
  stepCommit,
  items,
  unloaded,
  collapsed,
  setCollapsed,
  isReviewed,
  toggleReviewed,
  pending,
  setFeedback,
  showSidebar,
  closeMobile,
}: NavigationOptions) {
  const [tab, setTab] = useState<"files" | "comments">("files");
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState("");
  const [navigationTarget, setNavigationTarget] = useState({
    path: "",
    sequence: 0,
  });
  const [commentNavigationTarget, setCommentNavigationTarget] = useState<{
    path: string;
    side: ReviewComment["side"];
    start: number;
    end: number;
    sequence: number;
  } | null>(null);
  const [changeNavigationTarget, setChangeNavigationTarget] = useState<{
    path: string;
    deletions: LineSpan | null;
    additions: LineSpan | null;
    sequence: number;
  } | null>(null);
  const [changeAnnouncement, setChangeAnnouncement] = useState("");
  const closedKey = `closed-folders:${stream}`;
  const [closed, setClosed] = useState(() => sessionPaths(closedKey));
  useEffect(() => {
    saveSessionPaths(closedKey, closed);
  }, [closedKey, closed]);
  const [filteredClosed, setFilteredClosed] = useState(new Set<string>());
  const hideReviewedKey = `hide-reviewed:${stream}`;
  const [hideReviewed, setHideReviewed] = useState(() =>
    sessionFlag(hideReviewedKey),
  );
  useEffect(() => {
    saveSessionFlag(hideReviewedKey, hideReviewed);
  }, [hideReviewedKey, hideReviewed]);
  const [pendingComment, setPendingComment] = useState<ReviewComment | null>(
    null,
  );
  const [pendingFile, setPendingFile] = useState<{
    path: string;
    scope: DiffMode;
  } | null>(null);
  const viewer = useRef<CodeViewHandle<CommentAnnotation, undefined>>(null);
  const search = useRef<HTMLInputElement>(null);
  const matchingFiles = useMemo(
    () =>
      filesInTreeOrder(
        repository?.files.filter((file) =>
          file.path.toLowerCase().includes(filter.toLowerCase()),
        ) ?? [],
      ),
    [repository, filter],
  );
  // The tree and diff list share one visible set.
  const files = useMemo(
    () =>
      hideReviewed
        ? matchingFiles.filter((file) => !isReviewed(file))
        : matchingFiles,
    [matchingFiles, hideReviewed, isReviewed],
  );
  // A selected file hidden as reviewed hands off to its next visible neighbor.
  const activePath = useMemo(() => {
    if (files.some((file) => file.path === selected)) return selected;
    const index = matchingFiles.findIndex((file) => file.path === selected);
    const visible = (file: ChangedFile) => files.includes(file);
    const neighbor =
      index < 0
        ? undefined
        : (matchingFiles.slice(index + 1).find(visible) ??
          matchingFiles.slice(0, index).findLast(visible));
    return (neighbor ?? files[0])?.path ?? "";
  }, [files, matchingFiles, selected]);
  const selectVisibleFile = useCallback((path: string) => {
    setSelected(path);
    const expandParents = (previous: Set<string>) => {
      const parents = ancestorPaths(path);
      if (!parents.some((parent) => previous.has(parent))) return previous;
      const next = new Set(previous);
      for (const parent of parents) next.delete(parent);
      return next;
    };
    setClosed(expandParents);
    setFilteredClosed(expandParents);
    requestAnimationFrame(() => {
      document
        .querySelector<HTMLElement>(
          `#file-tree [data-path="${CSS.escape(path)}"]`,
        )
        ?.scrollIntoView({ block: "nearest" });
    });
  }, []);
  // Scrolling never changes selection; only explicit navigation does.
  // Hiding the selected file continues the review at its neighbor.
  useEffect(() => {
    if (
      !hideReviewed ||
      !activePath ||
      activePath === selected ||
      !matchingFiles.some((file) => file.path === selected)
    )
      return;
    selectVisibleFile(activePath);
    requestAnimationFrame(() => {
      viewer.current?.scrollTo({
        type: "item",
        id: activePath,
        align: "start",
        behavior: "instant",
      });
    });
  }, [hideReviewed, activePath, selected, matchingFiles, selectVisibleFile]);
  const revealFile = useCallback(
    (path: string) => {
      selectVisibleFile(path);
      setCollapsed((previous) => {
        if (!previous.has(path)) return previous;
        const next = new Set(previous);
        next.delete(path);
        return next;
      });
      closeMobile();
    },
    [selectVisibleFile, closeMobile, setCollapsed],
  );
  const selectFile = useCallback(
    (path: string) => {
      revealFile(path);
      setNavigationTarget((previous) => ({
        path,
        sequence: previous.sequence + 1,
      }));
      requestAnimationFrame(() => {
        viewer.current?.scrollTo({
          type: "item",
          id: path,
          align: "start",
          behavior: "instant",
        });
      });
    },
    [revealFile],
  );
  // Marking a file scrolled past its top collapses it above the viewport;
  // restore its header there so the next file shows from its top. Advancing
  // selects the next file instead. Hidden files hand off in navigation.
  const toggleFileReviewed = useCallback(
    async (file: ChangedFile, { advance }: { advance: boolean }) => {
      const marking = !isReviewed(file);
      const instance = viewer.current?.getInstance();
      const top = instance?.getTopForItem(file.path);
      const scrolledInto =
        !hideReviewed &&
        marking &&
        top !== undefined &&
        instance !== undefined &&
        instance.getScrollTop() > top;
      if (!(await toggleReviewed(file))) return;
      const index = files.findIndex((entry) => entry.path === file.path);
      const next = index < 0 ? undefined : files[index + 1];
      if (advance && marking && !hideReviewed && next) {
        selectFile(next.path);
        return;
      }
      if (!scrolledInto) return;
      requestAnimationFrame(() => {
        viewer.current?.scrollTo({
          type: "item",
          id: file.path,
          align: "start",
          behavior: "instant",
        });
      });
    },
    [isReviewed, hideReviewed, toggleReviewed, files, selectFile],
  );
  // Change blocks of the visible, expanded files, in viewer order.
  const stops = useMemo(
    () =>
      changeStops(
        files.map((file) => file.path),
        items,
        collapsed,
      ),
    [files, items, collapsed],
  );
  const changeCursor = useRef<StopKey | null>(null);
  // An end-of-changes notice clears once navigation moves again.
  const changeNotice = useRef(false);
  const stepChange = useCallback(
    (delta: -1 | 1) => {
      const instance = viewer.current?.getInstance();
      if (!instance) return;
      if (stops.length === 0) {
        changeNotice.current = true;
        setFeedback("No expanded changes to navigate.");
        return;
      }
      const rendered = instance.getRenderedItems();
      const header =
        rendered[0]?.element.shadowRoot
          ?.querySelector<HTMLElement>("[data-diffs-header]")
          ?.getBoundingClientRect().height ?? 0;
      const scrollTop = instance.getScrollTop();
      const viewport = {
        top: scrollTop + header,
        bottom: scrollTop + instance.getHeight(),
      };
      // Rendered files measure their rows; others sit at their file's top.
      const rect = (stop: ChangeStop): StopRect & { measured: boolean } => {
        const fileTop = instance.getTopForItem(stop.path) ?? 0;
        const item = rendered.find((entry) => entry.id === stop.path);
        if (item?.type === "diff") {
          const rows = [
            stop.deletions && {
              first: item.instance.getLinePosition(
                stop.deletions.start,
                "deletions",
              ),
              last: item.instance.getLinePosition(
                stop.deletions.end,
                "deletions",
              ),
            },
            stop.additions && {
              first: item.instance.getLinePosition(
                stop.additions.start,
                "additions",
              ),
              last: item.instance.getLinePosition(
                stop.additions.end,
                "additions",
              ),
            },
          ].flatMap((side) =>
            side?.first && side.last
              ? [{ first: side.first, last: side.last }]
              : [],
          );
          if (rows.length > 0)
            return {
              top: fileTop + Math.min(...rows.map((row) => row.first.top)),
              bottom:
                fileTop +
                Math.max(...rows.map((row) => row.last.top + row.last.height)),
              measured: true,
            };
        }
        return { top: fileTop, bottom: fileTop, measured: false };
      };
      const target = stepChangeStop(
        stops,
        delta,
        viewport,
        rect,
        changeCursor.current,
      );
      if (!target) {
        changeNotice.current = true;
        setFeedback(
          delta === -1
            ? "No changes above."
            : unloaded > 0
              ? "No more loaded changes below. Load more files to continue."
              : "No more changes below.",
        );
        return;
      }
      if (changeNotice.current) {
        changeNotice.current = false;
        setFeedback("");
      }
      changeCursor.current = { path: target.path, index: target.index };
      selectVisibleFile(target.path);
      const shown = rect(target);
      // A block already fully on screen stays put; the cue marks it.
      if (
        !shown.measured ||
        shown.top < viewport.top ||
        shown.bottom > viewport.bottom
      )
        viewer.current?.scrollTo({
          type: "line",
          id: target.path,
          lineNumber: target.anchor.lineNumber,
          side: target.anchor.side,
          align: "start",
          behavior: "instant",
        });
      setChangeNavigationTarget((previous) => ({
        path: target.path,
        deletions: target.deletions,
        additions: target.additions,
        sequence: (previous?.sequence ?? 0) + 1,
      }));
      setChangeAnnouncement(
        `Change ${target.index + 1} of ${target.count} in ${target.path}`,
      );
    },
    [stops, unloaded, setFeedback, selectVisibleFile],
  );
  const canStepChanges = stops.length > 0;

  // File stepping shares change navigation's origin rule: the selected file
  // while any of it is on screen, otherwise the viewport's top.
  const fileNotice = useRef(false);
  const stepFile = useCallback(
    (delta: -1 | 1) => {
      const instance = viewer.current?.getInstance();
      if (!instance) return;
      // Pierre measures loaded previews; files after them wait for Load more.
      const tops = files.flatMap((file) => {
        const top = instance.getTopForItem(file.path);
        return top === undefined ? [] : [{ path: file.path, top }];
      });
      // A file spans from its top to the next file's top.
      const placed = tops.map((file, index) => ({
        ...file,
        bottom: tops[index + 1]?.top ?? Number.POSITIVE_INFINITY,
      }));
      const scrollTop = instance.getScrollTop();
      const target = stepInViewport(
        placed,
        delta,
        { top: scrollTop, bottom: scrollTop + instance.getHeight() },
        (file) => file,
        placed.findIndex((file) => file.path === activePath),
      );
      if (!target) {
        fileNotice.current = true;
        setFeedback(
          delta === -1
            ? "No previous file."
            : unloaded > 0
              ? "No more loaded files below. Load more files to continue."
              : "No more files below.",
        );
        return;
      }
      if (fileNotice.current) {
        fileNotice.current = false;
        setFeedback("");
      }
      selectFile(target.path);
      setChangeAnnouncement(
        `File ${files.findIndex((file) => file.path === target.path) + 1} of ${files.length}: ${target.path}`,
      );
    },
    [files, activePath, unloaded, setFeedback, selectFile],
  );
  const canStepFiles = files.length > 0;

  const navigateComment = useCallback(
    (comment: ReviewComment) => {
      // Comments open in their scope; a commit view shows none.
      if (
        (comment.scope !== mode || viewingCommit) &&
        scopes.includes(comment.scope)
      ) {
        setPendingComment(comment);
        changeMode(comment.scope);
        return;
      }
      if (!commentTarget(comment, repository)) {
        setTab("comments");
        showSidebar();
        setFeedback(
          "This comment's file is not in this diff. Its original code is preserved in the review sidebar.",
        );
        return;
      }
      setFilter("");
      setPendingComment(comment);
    },
    [
      mode,
      viewingCommit,
      scopes,
      changeMode,
      repository,
      showSidebar,
      setFeedback,
    ],
  );
  // Navigate only after the requested scope and its parsed previews are installed.
  useEffect(() => {
    if (!pendingComment || busy || repository?.mode !== pendingComment.scope)
      return;
    const target = commentTarget(pendingComment, repository);
    if (target) {
      if (filter) {
        setFilter("");
        return;
      }
      if (!files.some((file) => file.path === target.path)) {
        setHideReviewed(false);
        return;
      }
      revealFile(target.path);
      const frame = requestAnimationFrame(() => {
        if (!target.range) {
          viewer.current?.scrollTo({
            type: "item",
            id: target.path,
            align: "start",
            behavior: "instant",
          });
          setNavigationTarget((previous) => ({
            path: target.path,
            sequence: previous.sequence + 1,
          }));
          setCommentNavigationTarget(null);
        } else {
          const { side, start, end } = target.range;
          viewer.current?.scrollTo({
            type: "range",
            id: target.path,
            range: { start, end, side },
            align: "center",
            behavior: "instant",
          });
          setCommentNavigationTarget((previous) => ({
            path: target.path,
            side,
            start,
            end,
            sequence: (previous?.sequence ?? 0) + 1,
          }));
        }
        setPendingComment(null);
      });
      return () => cancelAnimationFrame(frame);
    }
    setTab("comments");
    setPendingComment(null);
  }, [pendingComment, busy, repository, filter, files, revealFile]);

  // Leaves a commit view for the file in a scope's diff.
  const openInScope = useCallback(
    (path: string, scope: DiffMode) => {
      setPendingFile({ path, scope });
      changeMode(scope);
    },
    [changeMode],
  );
  useEffect(() => {
    if (!pendingFile || busy || repository?.mode !== pendingFile.scope) return;
    setPendingFile(null);
    const target = repository.files.find(
      (file) => file.path === pendingFile.path,
    );
    if (!target) {
      setFeedback(`${pendingFile.path} has no net changes in this comparison.`);
      return;
    }
    setFilter("");
    if (hideReviewed && isReviewed(target)) setHideReviewed(false);
    selectFile(target.path);
  }, [
    pendingFile,
    busy,
    repository,
    hideReviewed,
    isReviewed,
    selectFile,
    setFeedback,
  ]);

  const showOutdatedComments = useCallback(() => {
    setTab("comments");
    showSidebar();
    requestAnimationFrame(() =>
      document
        .getElementById("outdated-comments")
        ?.scrollIntoView({ block: "start" }),
    );
  }, [showSidebar]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (
        event
          .composedPath()
          .some(
            (target) =>
              target instanceof HTMLInputElement ||
              target instanceof HTMLTextAreaElement ||
              (target instanceof HTMLElement && target.isContentEditable),
          ) ||
        event.metaKey ||
        event.ctrlKey ||
        !event.altKey
      )
        return;
      // Physical bracket keys, so layouts that type other characters work.
      if (event.code === "BracketLeft" || event.code === "BracketRight") {
        const delta = event.code === "BracketLeft" ? -1 : 1;
        if (!event.shiftKey) {
          event.preventDefault();
          stepChange(delta);
        } else if (viewingCommit) {
          event.preventDefault();
          if (!stepCommit(delta))
            setFeedback(
              delta === -1
                ? "This is the oldest captured commit."
                : "This is the newest commit.",
            );
        }
        return;
      }
      // Other Alt+Shift chords step between reviews (review-navigation.tsx).
      if (event.shiftKey) return;
      if (event.code === "Slash") {
        event.preventDefault();
        showSidebar();
        setTab("files");
        requestAnimationFrame(() => search.current?.focus());
      }
      if (event.code === "KeyR") {
        event.preventDefault();
        if (refreshEnabled) refresh();
      }
      if (event.code === "KeyJ" || event.code === "KeyK") {
        event.preventDefault();
        stepFile(event.code === "KeyJ" ? 1 : -1);
      }
      // Commit views and public previews are read-only.
      if (marking && event.code === "KeyM") {
        event.preventDefault();
        const file = files.find((entry) => entry.path === activePath);
        // A held key would flip the mark back and forth.
        if (!file || event.repeat || pending.has(file.id) || pending.has("*"))
          return;
        void toggleFileReviewed(file, { advance: true });
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [
    files,
    activePath,
    showSidebar,
    refresh,
    refreshEnabled,
    stepFile,
    viewingCommit,
    marking,
    stepCommit,
    setFeedback,
    pending,
    toggleFileReviewed,
    stepChange,
  ]);

  return useMemo(
    () => ({
      tab,
      setTab,
      filter,
      setFilter,
      hideReviewed,
      setHideReviewed,
      matchingFiles,
      files,
      activePath,
      navigationTarget,
      commentNavigationTarget,
      changeNavigationTarget,
      changeAnnouncement,
      stepChange,
      canStepChanges,
      stepFile,
      canStepFiles,
      closed,
      setClosed,
      filteredClosed,
      setFilteredClosed,
      selectFile,
      toggleFileReviewed,
      showOutdatedComments,
      search,
      viewer,
      navigateComment,
      openInScope,
    }),
    [
      tab,
      filter,
      hideReviewed,
      matchingFiles,
      files,
      activePath,
      navigationTarget,
      commentNavigationTarget,
      changeNavigationTarget,
      changeAnnouncement,
      stepChange,
      canStepChanges,
      stepFile,
      canStepFiles,
      closed,
      filteredClosed,
      selectFile,
      toggleFileReviewed,
      showOutdatedComments,
      navigateComment,
      openInScope,
    ],
  );
}
