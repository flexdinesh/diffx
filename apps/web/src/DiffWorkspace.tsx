import { emptyComparison } from "./diff-mode.ts";
import { DiffSkeleton } from "./WorkspaceLoading.tsx";
import type {
  CodeViewItem,
  DiffLineAnnotation,
  FileDiffMetadata,
  LineAnnotation,
  SelectedLineRange,
} from "@pierre/diffs";
import {
  CodeView,
  type CodeViewReactOptions,
  useWorkerPool,
} from "@pierre/diffs/react";
import { MessageSquareIcon } from "lucide-react";
import {
  api,
  errorDetail,
  type ApiRepositoryDiff as RepositoryDiff,
} from "@diffx/api";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { DisclosureIcon } from "@/components/disclosure";
import { EmptyState } from "@/components/empty-state";
import { IconButton } from "@/components/icon-button";
import { Notice } from "@/components/notice";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  togglePath,
  useDiffSource,
  useDiffCollapse,
  useDraft,
  useNavigation,
  useReviewState,
  useReviewedFilesState,
} from "./app-state.tsx";
import { useAppearance } from "./appearance-context.tsx";
import { CopyPathButton } from "./CopyPathButton.tsx";
import { DiffToolbar } from "./DiffToolbar.tsx";
import { DiffComparison } from "./DiffComparison.tsx";
import { themesFor } from "./display-options.ts";
import { DraftComment, ReviewCommentCard } from "./review.tsx";
import {
  commentInFile,
  commentStale,
  placedInline,
  placedOutdated,
  placementFor,
  type CommentAnnotation,
} from "./review-model.ts";
import { ServerStatus } from "./ServerStatus.tsx";
import { SettingsButton } from "./SettingsButton.tsx";
import { WorkspaceFeedback } from "./WorkspaceFeedback.tsx";
import { capabilityEnabled, useSession } from "./session-context.tsx";

const NAVIGATION_CUE_MS = 1_000;

type CommentLineRange = {
  side: "deletions" | "additions";
  start: number;
  end: number;
};

function markCommentedLines(
  root: ParentNode,
  ranges: readonly CommentLineRange[],
) {
  for (const element of root.querySelectorAll<HTMLElement>(
    "[data-commented-line]",
  ))
    element.removeAttribute("data-commented-line");
  for (const range of ranges) {
    const column = root.querySelector<HTMLElement>(
      `[data-code][data-${range.side}]`,
    );
    if (!column) continue;
    for (const element of column.querySelectorAll<HTMLElement>(
      "[data-line], [data-column-number]",
    )) {
      const value =
        element.getAttribute("data-line") ??
        element.getAttribute("data-column-number");
      const line = Number(value);
      if (line < range.start || line > range.end) continue;
      const lineType = element.getAttribute("data-line-type");
      const tone =
        lineType === "change-addition"
          ? "addition"
          : lineType === "change-deletion"
            ? "deletion"
            : "context";
      element.setAttribute("data-commented-line", tone);
    }
  }
}

function selectionLabel(prefix: string, range: SelectedLineRange | null) {
  if (!range) return "";
  const start = Math.min(range.start, range.end);
  const end = Math.max(range.start, range.end);
  return start === end
    ? `${prefix} line ${start}`
    : `${prefix} lines ${start}–${end}`;
}

async function loadDiffFiles(
  contextId: string,
  fileDiff: FileDiffMetadata,
  repository: RepositoryDiff,
) {
  const file = repository.files.find((entry) => entry.path === fileDiff.name);
  if (!file) throw new Error("File is no longer in this diff");
  const { data: body, error } = await api.GET(
    "/api/v2/contexts/{contextId}/diffs/{diffId}/files/{fileId}/contents",
    {
      params: {
        path: { contextId, diffId: repository.id, fileId: file.id },
        query: {
          scope: repository.mode,
          fileVersion: file.fingerprint,
          versionId: repository.versionId,
        },
      },
    },
  );
  if (!body)
    throw new Error(errorDetail(error, "Unable to load full file context"));
  return {
    oldFile: {
      name: file.oldPath ?? file.path,
      contents: body.before,
    },
    newFile: { name: file.path, contents: body.after },
  };
}

// Pierre needs numeric versions. Cache them by preview identity and annotation
// contents so typing in a React comment editor doesn't rebuild the diff DOM.
function itemVersions() {
  const versions = new Map<
    string,
    {
      item: CodeViewItem<CommentAnnotation>;
      signature: string;
      version: number;
    }
  >();
  let next = 0;
  return (item: CodeViewItem<CommentAnnotation>, signature: string) => {
    const previous = versions.get(item.id);
    if (previous?.item === item && previous.signature === signature)
      return previous.version;
    const version = ++next;
    versions.set(item.id, { item, signature, version });
    return version;
  };
}

// Trailing space lets any file, including the last, scroll to the top.
function renderScrollEnd() {
  return <div className="diff-scroll-end" aria-hidden="true" />;
}

// Keep Pierre's annotation versions and event callbacks local to its renderer.
export function DiffWorkspace() {
  const {
    diff,
    repository,
    piped,
    mode,
    refreshEnabled,
    scopeAvailable,
    requestedScope,
  } = useDiffSource();
  const session = useSession();
  const { id: contextId, capabilities } = session;
  const { theme, themePreference, diffTheme, lineDiffType, layout, wrap } =
    useAppearance();
  // Stable per choice: theme definitions are module constants.
  const codeThemes = themesFor(diffTheme, themePreference);
  const { collapsed, setCollapsed } = useDiffCollapse();
  const {
    files,
    followScroll,
    filter,
    setFilter,
    matchingFiles,
    setHideReviewed,
    navigationTarget,
    commentNavigationTarget,
    navigateComment,
    viewer,
  } = useNavigation();
  const { isReviewed, toggleReviewed, pending } = useReviewedFilesState();
  const { draft } = useDraft();
  const review = useReviewState();
  const commentsEnabled = capabilityEnabled(capabilities.review.comments);
  const contentsEnabled = capabilityEnabled(capabilities.files.contents);
  const lineMetric = useRef<HTMLSpanElement>(null);
  const viewport = useRef<HTMLDivElement>(null);
  const [scrollbarHeight, setScrollbarHeight] = useState(0);
  const [viewerMetrics, setViewerMetrics] = useState<{
    lineHeight: number;
    spacing: number;
  }>();
  const lineHeight = viewerMetrics?.lineHeight;
  const fileSpacing = viewerMetrics?.spacing ?? 12;
  const [selectionFeedback, setSelectionFeedback] = useState("");
  const workerPool = useWorkerPool();
  useEffect(() => {
    if (!workerPool) return;
    void workerPool
      .setRenderOptions({ theme: codeThemes, lineDiffType })
      .catch((error: unknown) => console.error(error));
  }, [workerPool, codeThemes, lineDiffType]);
  // Virtual scroll offsets must use the same row height and spacing as our CSS.
  // Observe a sizing probe so browser font preferences also stay in sync.
  useLayoutEffect(() => {
    const element = lineMetric.current;
    if (!element) return;
    const measure = () => {
      const scroller = viewport.current;
      if (scroller) {
        document.documentElement.style.setProperty(
          "--diff-viewport-inset",
          `${scroller.offsetWidth - scroller.clientWidth}px`,
        );
        document.documentElement.style.setProperty(
          "--diff-viewport-height",
          `${scroller.clientHeight}px`,
        );
      }
      const { height, width } = element.getBoundingClientRect();
      setViewerMetrics((current) =>
        current?.lineHeight === height && current.spacing === width
          ? current
          : { lineHeight: height, spacing: width },
      );
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    if (viewport.current) observer.observe(viewport.current);
    return () => {
      observer.disconnect();
      document.documentElement.style.removeProperty("--diff-viewport-inset");
      document.documentElement.style.removeProperty("--diff-viewport-height");
    };
  }, []);
  const versionFor = useMemo(itemVersions, []);
  const allFiles = repository?.files ?? [];
  const reviewedCount = allFiles.filter(isReviewed).length;
  const items = useMemo(
    () =>
      files.flatMap((file): CodeViewItem<CommentAnnotation>[] => {
        const item = diff.items.get(file.path);
        if (!item) return [];
        const annotations: DiffLineAnnotation<CommentAnnotation>[] = (
          commentsEnabled ? review.comments : []
        )
          .filter(
            (comment) =>
              commentInFile(comment, file) &&
              comment.scope === mode &&
              comment.id !== draft?.id,
          )
          .flatMap((comment): DiffLineAnnotation<CommentAnnotation>[] => {
            const placement = placementFor(comment, repository);
            const fileSide = file.status === "D" ? "deletions" : "additions";
            // Inline comments sit on their (possibly moved) lines; outdated
            // ones stay at file level with their original code.
            if (placedInline(placement.applicability))
              return [
                {
                  side: comment.target === "file" ? fileSide : placement.side,
                  lineNumber: comment.target === "file" ? 0 : placement.end,
                  metadata: { kind: "saved", comment },
                },
              ];
            return placedOutdated(placement.applicability)
              ? [
                  {
                    side: fileSide,
                    lineNumber: 0,
                    metadata: { kind: "saved", comment },
                  },
                ]
              : [];
          });
        if (
          draft &&
          draft.path === file.path &&
          draft.scope === mode &&
          draft.fingerprint === file.fingerprint
        )
          annotations.push({
            side:
              draft.target === "file" && file.status === "D"
                ? "deletions"
                : draft.side,
            lineNumber: draft.end,
            metadata: { kind: "draft" },
          });
        const state = {
          collapsed: collapsed.has(file.path),
          version: versionFor(
            item,
            `${collapsed.has(file.path)}:${JSON.stringify(annotations)}`,
          ),
        };
        return [
          item.type === "diff"
            ? { ...item, ...state, ...(commentsEnabled ? { annotations } : {}) }
            : {
                ...item,
                ...state,
                ...(commentsEnabled
                  ? {
                      annotations: annotations
                        .filter((annotation) => annotation.lineNumber === 0)
                        .map(
                          ({
                            lineNumber,
                            metadata,
                          }): LineAnnotation<CommentAnnotation> =>
                            metadata.kind === "saved"
                              ? { lineNumber, metadata }
                              : { lineNumber, metadata },
                        ),
                    }
                  : {}),
              },
        ];
      }),
    [
      files,
      diff.items,
      review.comments,
      repository,
      draft,
      mode,
      collapsed,
      versionFor,
      commentsEnabled,
    ],
  );

  // Keep renderer options stable while event callbacks see current React state.
  const actions = useRef({
    repository,
    begin: review.begin,
    draft,
    navigateComment,
  });
  useLayoutEffect(() => {
    actions.current = {
      repository,
      begin: review.begin,
      draft,
      navigateComment,
    };
  });
  useLayoutEffect(() => {
    if (
      draft &&
      draft.target !== "file" &&
      draft.scope === mode &&
      files.some(
        (file) =>
          file.path === draft.path && file.fingerprint === draft.fingerprint,
      )
    ) {
      viewer.current?.setSelectedLines({
        id: draft.path,
        range: { start: draft.start, end: draft.end, side: draft.side },
      });
    } else viewer.current?.clearSelectedLines();
  }, [draft, files, mode, viewer]);
  useEffect(() => {
    if (!navigationTarget.path) return;
    let frame = 0;
    let timeout = 0;
    let target: HTMLElement | undefined;
    const deadline = performance.now() + NAVIGATION_CUE_MS;
    const highlight = () => {
      const item = viewer.current
        ?.getInstance()
        ?.getRenderedItems()
        .find((rendered) => rendered.id === navigationTarget.path);
      target =
        item?.element.shadowRoot?.querySelector<HTMLElement>(
          "[data-diffs-header]",
        ) ?? undefined;
      if (!target) {
        if (performance.now() < deadline)
          frame = requestAnimationFrame(highlight);
        return;
      }
      target.setAttribute("data-navigation-target", "");
      timeout = window.setTimeout(() => {
        target?.removeAttribute("data-navigation-target");
      }, NAVIGATION_CUE_MS);
    };
    frame = requestAnimationFrame(highlight);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(timeout);
      target?.removeAttribute("data-navigation-target");
    };
  }, [navigationTarget, viewer]);
  useEffect(() => {
    if (!commentNavigationTarget) return;
    let frame = 0;
    let timeout = 0;
    let targets: HTMLElement[] = [];
    const deadline = performance.now() + NAVIGATION_CUE_MS;
    const highlight = () => {
      const item = viewer.current
        ?.getInstance()
        ?.getRenderedItems()
        .find((rendered) => rendered.id === commentNavigationTarget.path);
      const column = item?.element.shadowRoot?.querySelector<HTMLElement>(
        `[data-code][data-${commentNavigationTarget.side}]`,
      );
      targets = Array.from(
        column?.querySelectorAll<HTMLElement>("[data-line]") ?? [],
      ).filter((element) => {
        const line = Number(element.getAttribute("data-line"));
        return (
          line >= commentNavigationTarget.start &&
          line <= commentNavigationTarget.end
        );
      });
      if (targets.length === 0) {
        if (performance.now() < deadline)
          frame = requestAnimationFrame(highlight);
        return;
      }
      for (const target of targets)
        target.setAttribute("data-comment-navigation-target", "");
      timeout = window.setTimeout(() => {
        for (const target of targets)
          target.removeAttribute("data-comment-navigation-target");
      }, NAVIGATION_CUE_MS);
    };
    frame = requestAnimationFrame(highlight);
    return () => {
      cancelAnimationFrame(frame);
      clearTimeout(timeout);
      for (const target of targets)
        target.removeAttribute("data-comment-navigation-target");
    };
  }, [commentNavigationTarget, viewer]);
  const options = useMemo<CodeViewReactOptions<CommentAnnotation, undefined>>(
    () => ({
      theme: codeThemes,
      themeType: theme,
      diffStyle: layout,
      lineDiffType,
      overflow: wrap ? "wrap" : "scroll",
      diffIndicators: "bars",
      hunkSeparators: contentsEnabled ? "line-info-basic" : "metadata",
      expansionLineCount: 20,
      collapsedContextThreshold: 3,
      ...(contentsEnabled
        ? {
            loadDiffFiles(fileDiff) {
              const current = actions.current.repository;
              if (!current) throw new Error("Repository is unavailable");
              return loadDiffFiles(contextId, fileDiff, current);
            },
          }
        : {}),
      unsafeCSS: `
        [data-diff], [data-code] {
          padding-bottom: 0;
        }
        /* Native scrollbar tracks are part of the code surface. */
        [data-code]:has(> [data-content] > :is([data-line], [data-no-newline]):last-child[data-line-type="change-addition"]) {
          background-color: var(--diffs-bg-addition);
        }
        [data-code]:has(> [data-content] > :is([data-line], [data-no-newline]):last-child[data-line-type="change-deletion"]) {
          background-color: var(--diffs-bg-deletion);
        }
        [data-diffs-header] {
          height: calc(2 * var(--space-4) + var(--space-1));
          min-height: calc(2 * var(--space-4) + var(--space-1));
          background: var(--diff-file-header);
          box-shadow: inset 0 -1px var(--border);
        }
        [data-change-icon="change"] { color: var(--warning); }
        [data-diffs-header][data-navigation-target] {
          animation: navigation-target ${NAVIGATION_CUE_MS}ms ease-out;
        }
        @keyframes navigation-target {
          0%, 35% {
            background: var(--accent-bg);
            box-shadow:
              inset 3px 0 var(--accent),
              inset 0 -1px var(--border);
          }
          100% {
            background: var(--diff-file-header);
            box-shadow:
              inset 0 0 transparent,
              inset 0 -1px var(--border);
          }
        }
        @media (prefers-reduced-motion: reduce) {
          [data-diffs-header][data-navigation-target] {
            animation: none;
            background: var(--accent-bg);
            box-shadow:
              inset 3px 0 var(--accent),
              inset 0 -1px var(--border);
          }
        }
        [data-selected-line][data-hovered] {
          --diffs-computed-hovered-line-bg: var(--diffs-computed-selected-line-bg);
        }
        [data-commented-line="context"] {
          --diffs-line-bg: color-mix(
            in srgb,
            var(--diffs-computed-diff-line-bg) 84%,
            var(--review-anchor)
          );
        }
        [data-commented-line="addition"] {
          --diffs-line-bg: color-mix(
            in srgb,
            var(--diffs-computed-diff-line-bg) 76%,
            color-mix(in srgb, var(--diffs-addition-base) 72%, black)
          );
        }
        [data-commented-line="deletion"] {
          --diffs-line-bg: color-mix(
            in srgb,
            var(--diffs-computed-diff-line-bg) 76%,
            color-mix(in srgb, var(--diffs-deletion-base) 72%, black)
          );
        }
        [data-comment-navigation-target] {
          animation: comment-navigation-target ${NAVIGATION_CUE_MS}ms ease-out;
        }
        @keyframes comment-navigation-target {
          0%, 40% {
            box-shadow: inset 3px 0 var(--accent);
            filter: saturate(1.25) brightness(0.92);
          }
          100% {
            box-shadow: inset 0 0 transparent;
            filter: none;
          }
        }
        @media (prefers-reduced-motion: reduce) {
          [data-comment-navigation-target] {
            animation: none;
            box-shadow: inset 3px 0 var(--accent);
            filter: saturate(1.25) brightness(0.92);
          }
        }
      `,
      stickyHeaders: true,
      itemMetrics: {
        // Native scrollbar tracks consume height even with zero code padding.
        paddingBottom: wrap ? 0 : scrollbarHeight,
        ...(lineHeight === undefined ? {} : { lineHeight }),
        // Matches the header's 2 * --space-4 + --space-1 height.
        diffHeaderHeight: fileSpacing * 3,
      },
      layout: {
        paddingTop: fileSpacing,
        paddingBottom: fileSpacing,
        gap: fileSpacing,
      },
      onPostRender(node, _instance, phase, context) {
        if (phase === "unmount") return;
        const columns = Array.from(
          (node.shadowRoot ?? node).querySelectorAll<HTMLElement>(
            "[data-code]",
          ),
        );
        if (!wrap && columns.length > 0 && !context.item.collapsed) {
          const height = Math.max(
            ...columns.map(
              (column) => column.offsetHeight - column.clientHeight,
            ),
          );
          setScrollbarHeight((current) =>
            current === height ? current : height,
          );
        }
        if (!commentsEnabled || context.item.type !== "diff") return;
        const currentDraft = actions.current.draft;
        const ranges = (context.item.annotations ?? []).flatMap(
          (annotation): CommentLineRange[] => {
            if (annotation.metadata.kind === "saved") {
              const { comment } = annotation.metadata;
              const placement = placementFor(
                comment,
                actions.current.repository,
              );
              return comment.target !== "file" &&
                placedInline(placement.applicability)
                ? [placement]
                : [];
            }
            return currentDraft && currentDraft.path === context.item.id
              ? [currentDraft]
              : [];
          },
        );
        markCommentedLines(node.shadowRoot ?? node, ranges);
      },
      ...(commentsEnabled
        ? {
            enableGutterUtility: true,
            lineHoverHighlight: "both",
            onGutterUtilityClick(range, context) {
              if (context.item.type !== "diff") return;
              const current = actions.current;
              if (current.draft) {
                current.navigateComment(current.draft);
                return;
              }
              const file = current.repository?.files.find(
                (file) => file.path === context.item.id,
              );
              if (file) current.begin(file, context.item.fileDiff, range);
            },
            onLineSelectionStart(range) {
              setSelectionFeedback(selectionLabel("Selecting", range));
            },
            onLineSelectionChange(range) {
              setSelectionFeedback(selectionLabel("Selecting", range));
            },
            onLineSelectionEnd(range) {
              setSelectionFeedback(selectionLabel("Selected", range));
            },
            onLineEnter(_event, context) {
              requestAnimationFrame(() =>
                context.element?.shadowRoot
                  ?.querySelector("[data-utility-button]")
                  ?.setAttribute("aria-label", "Add review comment"),
              );
            },
          }
        : {}),
    }),
    [
      contextId,
      theme,
      codeThemes,
      lineDiffType,
      layout,
      wrap,
      lineHeight,
      fileSpacing,
      scrollbarHeight,
      contentsEnabled,
      commentsEnabled,
    ],
  );

  const loadingChanges = scopeAvailable && !repository && !diff.notice;
  // Matching files with none visible were all hidden as reviewed.
  const allReviewedHidden = matchingFiles.length > 0;
  const empty = emptyComparison(session, mode);
  const emptyTitle = !scopeAvailable
    ? "Comparison unavailable"
    : !repository
      ? "Cannot load changes"
      : allReviewedHidden
        ? filter
          ? "All matching files reviewed"
          : "All files reviewed"
        : filter
          ? "No matching files"
          : piped
            ? "No file changes in this input"
            : empty.title;
  const emptyDescription = !scopeAvailable
    ? `The “${requestedScope}” scope was not collected in this snapshot. Choose an available comparison above.`
    : !repository
      ? "Check that the server is running, then refresh."
      : allReviewedHidden
        ? "Reviewed files are hidden."
        : filter
          ? "Try a different filename or clear the filter."
          : piped
            ? "Run a command that emits a Git patch, then pipe it into diffx."
            : empty.description;
  return (
    <main>
      <DiffToolbar />
      <WorkspaceFeedback />
      <Notice id="notice" tone="warning" hidden={!diff.notice}>
        {diff.notice}
      </Notice>
      <p className="visually-hidden" role="status" aria-live="polite">
        {selectionFeedback}
      </p>
      <div className="review-surface">
        <section id="viewer" aria-label="Code differences">
          <span
            ref={lineMetric}
            className="diff-line-metric"
            aria-hidden="true"
          />
          <CodeView
            className="diff-code-view"
            containerRef={viewport}
            ref={viewer}
            items={items}
            onScroll={(scrollTop, instance) => {
              const height = viewport.current?.clientHeight ?? 0;
              // Use Pierre's full item offsets, including unmounted files.
              // A file stays visible until its body (not just its header) leaves.
              const visible = items.filter((item, index) => {
                const top = instance.getTopForItem(item.id);
                const next = items[index + 1];
                const nextTop = next
                  ? instance.getTopForItem(next.id)
                  : undefined;
                const bottom =
                  nextTop === undefined ? Infinity : nextTop - fileSpacing;
                return (
                  top !== undefined &&
                  top < scrollTop + height &&
                  bottom > scrollTop
                );
              });
              followScroll(visible.map((item) => item.id));
            }}
            options={options}
            style={{ height: "100%", width: "100%", overflow: "auto" }}
            renderCodeViewFooter={renderScrollEnd}
            renderHeaderPrefix={(item) => (
              <span className="diff-collapse-gutter">
                <IconButton
                  label={`${item.collapsed ? "Expand" : "Collapse"} ${item.id}`}
                  hint={item.collapsed ? "Expand file" : "Collapse file"}
                  variant="subtle"
                  size="icon-xs"
                  aria-expanded={!item.collapsed}
                  onClick={() =>
                    setCollapsed((previous) => togglePath(previous, item.id))
                  }
                >
                  <DisclosureIcon open={!item.collapsed} />
                </IconButton>
              </span>
            )}
            renderHeaderFilenameSuffix={(item) => (
              <CopyPathButton value={item.id} label="Copy relative path" />
            )}
            renderHeaderMetadata={(item) => {
              const file = allFiles.find((file) => file.path === item.id);
              const outdated =
                commentsEnabled && file
                  ? review.comments.filter(
                      (comment) =>
                        comment.scope === mode &&
                        commentInFile(comment, file) &&
                        commentStale(comment, repository),
                    ).length
                  : 0;
              return file ? (
                <div className="flex items-center gap-1">
                  {outdated > 0 && (
                    <span
                      className="file-outdated-count"
                      title={`${outdated} outdated review ${outdated === 1 ? "comment" : "comments"} shown at the top of this file`}
                    >
                      {outdated} outdated
                    </span>
                  )}
                  <Button
                    type="button"
                    variant="outline-check"
                    size="sm"
                    aria-label={`Mark ${file.path} ${isReviewed(file) ? "unreviewed" : "reviewed"}`}
                    aria-pressed={isReviewed(file)}
                    disabled={pending.has(file.id) || pending.has("*")}
                    aria-busy={pending.has(file.id)}
                    onClick={() => toggleReviewed(file)}
                  >
                    <svg
                      className="review-checkbox"
                      viewBox="0 0 16 16"
                      aria-hidden="true"
                    >
                      <rect x="2" y="2" width="12" height="12" />
                      {isReviewed(file) && <path d="m4.5 8 2.5 2.5 4.5-5" />}
                    </svg>
                    Reviewed
                  </Button>

                  {commentsEnabled && (
                    <IconButton
                      label={`Leave review comment on file ${file.path}`}
                      hint="Leave review comment on file"
                      variant="outline-muted"
                      size="icon-sm"
                      onClick={() => {
                        setCollapsed((previous) => {
                          const next = new Set(previous);
                          next.delete(file.path);
                          return next;
                        });
                        if (draft) navigateComment(draft);
                        else review.begin(file);
                      }}
                    >
                      <MessageSquareIcon aria-hidden="true" />
                    </IconButton>
                  )}
                </div>
              ) : null;
            }}
            renderAnnotation={(annotation) =>
              annotation.metadata.kind === "draft" ? (
                <DraftComment />
              ) : (
                <ReviewCommentCard comment={annotation.metadata.comment} />
              )
            }
          />
        </section>
        <div
          id="empty"
          role="status"
          hidden={files.length > 0}
          className={cn(
            "absolute inset-0 flex flex-col",
            loadingChanges
              ? "items-stretch justify-start"
              : "items-center justify-center p-(--page-gutter)",
          )}
          aria-busy={loadingChanges}
        >
          {loadingChanges ? (
            <>
              <DiffSkeleton />
              <p className="visually-hidden">Loading changes</p>
            </>
          ) : (
            <EmptyState
              size="page"
              title={emptyTitle}
              description={emptyDescription}
              action={
                allReviewedHidden ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setHideReviewed(false)}
                  >
                    Show reviewed files
                  </Button>
                ) : filter ? (
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => setFilter("")}
                  >
                    Clear filter
                  </Button>
                ) : undefined
              }
            />
          )}
        </div>
      </div>
      <footer className="main-footer">
        <DiffComparison />
        <span
          className="mobile-review-progress"
          aria-label={`${reviewedCount} of ${allFiles.length} files reviewed`}
        >
          {reviewedCount}/{allFiles.length} reviewed
        </span>
        <span className="footer-details">
          <span className="footer-shortcuts">
            <kbd>Alt+J</kbd> <kbd>Alt+K</kbd> files <kbd>Alt+/</kbd> filter
            {refreshEnabled && (
              <>
                {" "}
                <kbd>Alt+R</kbd> refresh
              </>
            )}
          </span>
          <span className="flex items-center gap-1">
            <ServerStatus />
            <SettingsButton />
          </span>
        </span>
      </footer>
    </main>
  );
}
