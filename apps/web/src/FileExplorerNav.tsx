import { useMemo } from "react";
import { EyeOffIcon, XIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Toggle } from "@/components/ui/toggle";
import {
  togglePath,
  useDiffSource,
  useNavigation,
  useReviewState,
  useReviewedFilesState,
} from "./app-state.tsx";
import { useSidebarState } from "./sidebar-context.tsx";
import { CommentsPanel } from "./CommentsPanel.tsx";
import { type CommentCount, FileNavigation } from "./file-navigation.tsx";
import { ancestorPaths } from "./file-tree.ts";
import { commentInFile, commentStale } from "./review-model.ts";
import { ChangeSummary, ReviewTools } from "./ReviewTools.tsx";
import { capabilityEnabled, useSession } from "./session-context.tsx";

export function FileExplorerNav() {
  const { repository, diff, mode } = useDiffSource();
  const { capabilities } = useSession();
  const {
    tab,
    setTab,
    filter,
    setFilter,
    hideReviewed,
    setHideReviewed,
    matchingFiles,
    files,
    activePath,
    closed,
    setClosed,
    filteredClosed,
    setFilteredClosed,
    selectFile,
    search,
  } = useNavigation();
  const sidebar = useSidebarState();
  const review = useReviewState();
  const { isReviewed } = useReviewedFilesState();
  const commentsEnabled = capabilityEnabled(capabilities.review.comments);
  const refreshEnabled = capabilityEnabled(capabilities.diff.refresh);
  const stagingMetadataEnabled = capabilityEnabled(
    capabilities.diff.stagingMetadata,
  );
  const allFiles = repository?.files ?? [];
  const folderPaths = useMemo(
    () => [...new Set(files.flatMap((file) => ancestorPaths(file.path)))],
    [files],
  );
  // Closing every root folder hides the whole tree, even after manual toggles.
  const foldersCollapsed =
    folderPaths.length > 0 &&
    folderPaths
      .filter((path) => !path.includes("/"))
      .every((path) => (filter ? filteredClosed : closed).has(path));
  // Counts follow renames to the viewed file and separate outdated comments.
  const commentCounts = useMemo(() => {
    const counts = new Map<string, CommentCount>();
    for (const comment of review.comments) {
      if (comment.scope !== mode) continue;
      const file = repository?.files.find((entry) =>
        commentInFile(comment, entry),
      );
      if (!file) continue;
      const count = counts.get(file.path) ?? { current: 0, outdated: 0 };
      if (commentStale(comment, repository)) count.outdated += 1;
      else count.current += 1;
      counts.set(file.path, count);
    }
    return counts;
  }, [review.comments, repository, mode]);
  const connection = !diff.connected
    ? diff.busy && !repository
      ? "Connecting…"
      : "Disconnected"
    : refreshEnabled
      ? "Refresh available"
      : "Fixed snapshot";
  return (
    <aside
      id="sidebar"
      className={`sidebar${sidebar.collapsed ? " is-collapsed" : ""}${sidebar.open && sidebar.view === "diff" ? " open" : ""}`}
      role={
        sidebar.mobile && sidebar.open && sidebar.view === "diff"
          ? "dialog"
          : undefined
      }
      hidden={sidebar.view !== "diff"}
      aria-label="Review navigation"
      aria-modal={
        sidebar.mobile && sidebar.open && sidebar.view === "diff"
          ? "true"
          : undefined
      }
    >
      <Tabs
        value={tab}
        onValueChange={(value) => {
          if (value === "files" || (value === "comments" && commentsEnabled))
            setTab(value);
        }}
        className="contents"
      >
        <div className="sidebar-tabs">
          <TabsList
            variant="line"
            className="sidebar-tab-list"
            aria-label="Sidebar view"
          >
            <TabsTrigger id="files-tab" value="files">
              <span className="sidebar-tab-label">Changes</span>
              <span id="file-count" className="count">
                {allFiles.length}
              </span>
            </TabsTrigger>
            {commentsEnabled && (
              <TabsTrigger id="comments-tab" value="comments">
                <span className="sidebar-tab-label">Review</span>
                <span id="comment-count" className="count">
                  {review.comments.length}
                </span>
              </TabsTrigger>
            )}
          </TabsList>
          <Button
            type="button"
            id="tree-toggle"
            className="tree-control"
            variant="ghost"
            size="icon"
            aria-label={`${foldersCollapsed ? "Expand" : "Collapse"} all folders`}
            title={`${foldersCollapsed ? "Expand" : "Collapse"} all folders`}
            disabled={folderPaths.length === 0}
            onClick={() => {
              const paths = new Set(foldersCollapsed ? [] : folderPaths);
              if (filter) setFilteredClosed(paths);
              else setClosed(paths);
            }}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <rect
                className="control-radius-shape"
                x="3"
                y="3"
                width="15"
                height="15"
              />
              <path d="M7 10.5h7M21 7v12a2 2 0 0 1-2 2H7" />
              {foldersCollapsed && <path d="M10.5 7v7" />}
            </svg>
          </Button>
          <Button
            type="button"
            className="tree-control mobile-only"
            variant="ghost"
            size="icon"
            aria-label="Close review sidebar"
            onClick={sidebar.closeMobile}
          >
            <XIcon aria-hidden="true" />
          </Button>
        </div>
        <TabsContent value="files" id="file-panel">
          <div className="file-filter-row">
            <div className="search-box">
              <svg viewBox="0 0 16 16" aria-hidden="true">
                <circle cx="7" cy="7" r="4.5" />
                <path d="m10.5 10.5 3 3" />
              </svg>
              <Input
                ref={search}
                id="search"
                type="search"
                placeholder="Filter files…"
                aria-label="Filter files"
                value={filter}
                onChange={(event) => {
                  setFilter(event.target.value);
                  setFilteredClosed(new Set());
                }}
              />
              <kbd>Alt+/</kbd>
            </div>
            <Toggle
              id="hide-reviewed"
              className="hide-reviewed-toggle"
              aria-label="Hide reviewed files"
              title="Hide reviewed files"
              pressed={hideReviewed}
              onPressedChange={setHideReviewed}
            >
              <EyeOffIcon aria-hidden="true" />
            </Toggle>
          </div>
          <p className="visually-hidden" role="status" aria-live="polite">
            {filter || hideReviewed
              ? `${files.length} of ${allFiles.length} changed files shown`
              : ""}
          </p>
          <FileNavigation
            files={files}
            selected={activePath}
            closed={filter ? filteredClosed : closed}
            emptyMessage={
              !allFiles.length
                ? "No changed files"
                : matchingFiles.length
                  ? "All files reviewed"
                  : "No matching files"
            }
            isReviewed={isReviewed}
            commentCounts={commentCounts}
            stagingMetadata={stagingMetadataEnabled}
            onToggle={(path) =>
              (filter ? setFilteredClosed : setClosed)((previous) =>
                togglePath(previous, path),
              )
            }
            onSelect={selectFile}
          />
          <div
            id="git-legend"
            className="git-legend"
            title="Git staging indicators"
            hidden={!stagingMetadataEnabled}
          >
            <span>
              <i className="staging-dot staged" aria-hidden="true" />
              Staged
            </span>
            <span>
              <i className="staging-dot both" aria-hidden="true" />
              Both
            </span>
            <span>
              <i className="staging-dot unstaged" aria-hidden="true" />
              Unstaged
            </span>
            <span title="Untracked file">U Untracked</span>
          </div>
        </TabsContent>
        {commentsEnabled && <CommentsPanel />}
      </Tabs>
      {tab === "comments" ? <ReviewTools /> : <ChangeSummary />}
      <div className="sidebar-footer">
        <span className="live-dot" />
        <span id="connection">{connection}</span>
        <span className="read-only">Git unchanged</span>
      </div>
    </aside>
  );
}
