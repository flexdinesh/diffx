import { useMemo } from "react";
import {
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  EyeOffIcon,
  XIcon,
} from "lucide-react";
import { CountBadge } from "@/components/count-badge";
import { IconButton } from "@/components/icon-button";
import { PanelHeader } from "@/components/panel-header";
import { SearchField } from "@/components/search-field";
import { StatusDot } from "@/components/status-dot";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Toggle } from "@/components/ui/toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
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
        <PanelHeader
          className="mb-2"
          actions={
            <>
              <IconButton
                label={`${foldersCollapsed ? "Expand" : "Collapse"} all folders`}
                variant="subtle"
                size="icon-sm"
                disabled={folderPaths.length === 0}
                onClick={() => {
                  const paths = new Set(foldersCollapsed ? [] : folderPaths);
                  if (filter) setFilteredClosed(paths);
                  else setClosed(paths);
                }}
              >
                {foldersCollapsed ? (
                  <ChevronsUpDownIcon aria-hidden="true" />
                ) : (
                  <ChevronsDownUpIcon aria-hidden="true" />
                )}
              </IconButton>
              <IconButton
                label="Close review sidebar"
                variant="subtle"
                size="icon-sm"
                className="lg:hidden"
                onClick={sidebar.closeMobile}
              >
                <XIcon aria-hidden="true" />
              </IconButton>
            </>
          }
        >
          <TabsList aria-label="Sidebar view">
            <TabsTrigger id="files-tab" value="files">
              <span className="truncate">Changes</span>
              <CountBadge id="file-count" count={allFiles.length} />
            </TabsTrigger>
            {commentsEnabled && (
              <TabsTrigger id="comments-tab" value="comments">
                <span className="truncate">Review</span>
                <CountBadge id="comment-count" count={review.comments.length} />
              </TabsTrigger>
            )}
          </TabsList>
        </PanelHeader>
        <TabsContent value="files" id="file-panel" className="file-panel">
          <div className="file-filter-row">
            <SearchField
              ref={search}
              label="Filter files"
              placeholder="Filter files…"
              shortcut={{ alt: true, key: "/" }}
              className="flex-1"
              value={filter}
              onChange={(event) => {
                setFilter(event.target.value);
                setFilteredClosed(new Set());
              }}
            />
            <Tooltip>
              <TooltipTrigger
                render={
                  <Toggle
                    id="hide-reviewed"
                    variant="outline"
                    aria-label="Hide reviewed files"
                    pressed={hideReviewed}
                    onPressedChange={setHideReviewed}
                  />
                }
              >
                <EyeOffIcon aria-hidden="true" />
              </TooltipTrigger>
              <TooltipContent>Hide reviewed files</TooltipContent>
            </Tooltip>
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
            className="git-legend"
            title="Git staging indicators"
            hidden={!stagingMetadataEnabled}
          >
            <span className="inline-flex items-center gap-1">
              <StatusDot size="md" fill="solid" />
              Staged
            </span>
            <span className="inline-flex items-center gap-1">
              <StatusDot size="md" fill="half" />
              Both
            </span>
            <span className="inline-flex items-center gap-1">
              <StatusDot size="md" fill="ring" />
              Unstaged
            </span>
            <span>U Untracked</span>
          </div>
        </TabsContent>
        {commentsEnabled && <CommentsPanel />}
      </Tabs>
      {tab === "comments" ? <ReviewTools /> : <ChangeSummary />}
      <div className="sidebar-footer">
        <StatusDot tone="success" size="sm" />
        <span id="connection">{connection}</span>
        <span className="read-only">Git unchanged</span>
      </div>
    </aside>
  );
}
