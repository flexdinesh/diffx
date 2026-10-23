import type { ApiChangedFile as ChangedFile } from "@diffx/api";
import {
  FolderIcon as Folder,
  FolderOpenIcon,
  MessageSquareIcon,
} from "lucide-react";
import type { KeyboardEvent } from "react";
import { DisclosureIcon } from "@/components/disclosure";
import { EmptyState } from "@/components/empty-state";
import { ListRow } from "@/components/list-row";
import { StatusDot } from "@/components/status-dot";
import {
  fileKind,
  gitDecoration,
  monograms,
  shapes,
} from "./file-decoration.ts";
import {
  ancestorPaths,
  buildFileTree,
  type FileTreeNode,
} from "./file-tree.ts";

function FolderIcon({ open }: { open: boolean }) {
  const Icon = open ? FolderOpenIcon : Folder;
  return (
    <Icon
      className="size-icon text-muted"
      aria-hidden="true"
      focusable="false"
    />
  );
}
function FileIcon({ path, reviewed }: { path: string; reviewed: boolean }) {
  const kind = fileKind(path);
  const monogram = monograms[kind];
  return (
    <span className="file-icon" data-kind={kind} aria-hidden="true">
      <svg
        viewBox="0 0 20 20"
        className={`file-type-icon${monogram ? " monogram-icon" : ""}`}
        aria-hidden="true"
        focusable="false"
      >
        {monogram ? (
          <>
            <path d="M2 2h16v16H2Z" />
            <text x="10" y="13.5">
              {monogram}
            </text>
          </>
        ) : (
          <path d={shapes[kind] ?? shapes.file} />
        )}
      </svg>
      {reviewed && <span className="file-reviewed-check">✓</span>}
    </span>
  );
}
function CommentIcon() {
  return <MessageSquareIcon aria-hidden="true" focusable="false" />;
}
function StatusBadge({
  file,
  stagingMetadata,
}: {
  file: ChangedFile;
  stagingMetadata: boolean;
}) {
  const decoration = gitDecoration(file);
  return (
    <span
      className="file-status"
      data-status={file.status}
      data-state={decoration.state}
      title={decoration.details}
      aria-hidden="true"
    >
      <span>{decoration.code}</span>
      {stagingMetadata &&
        (decoration.state === "staged" ? (
          <StatusDot size="md" fill="solid" />
        ) : decoration.state === "both" ? (
          <StatusDot size="md" fill="half" />
        ) : decoration.state === "unstaged" ? (
          <StatusDot size="md" fill="ring" />
        ) : null)}
    </span>
  );
}

export interface CommentCount {
  current: number;
  outdated: number;
}

interface Props {
  files: ChangedFile[];
  selected: string;
  closed: Set<string>;
  emptyMessage: string;
  isReviewed: (file: ChangedFile) => boolean;
  commentCounts: Map<string, CommentCount>;
  stagingMetadata: boolean;
  onToggle: (path: string) => void;
  onSelect: (path: string) => void;
}

export function FileNavigation({
  files,
  selected,
  closed,
  emptyMessage,
  isReviewed,
  commentCounts,
  stagingMetadata,
  onToggle,
  onSelect,
}: Props) {
  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    const focused = event.currentTarget;
    const buttons = [
      ...(focused
        .closest("nav")
        ?.querySelectorAll<HTMLButtonElement>("button") ?? []),
    ];
    const index = buttons.indexOf(focused);
    if (["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) {
      event.preventDefault();
      const next =
        event.key === "Home"
          ? 0
          : event.key === "End"
            ? buttons.length - 1
            : index + (event.key === "ArrowDown" ? 1 : -1);
      buttons[Math.max(0, Math.min(buttons.length - 1, next))]?.focus();
    }
    if (
      event.key === "ArrowRight" &&
      focused.getAttribute("aria-expanded") === "false"
    ) {
      event.preventDefault();
      focused.click();
    }
    if (event.key === "ArrowLeft") {
      event.preventDefault();
      if (focused.getAttribute("aria-expanded") === "true") focused.click();
      else {
        const parent = ancestorPaths(focused.dataset.treePath ?? "").at(-1);
        buttons.find((button) => button.dataset.treePath === parent)?.focus();
      }
    }
  }
  function renderNodes(nodes: FileTreeNode[], depth: number) {
    return (
      <ul className="tree-list">
        {nodes.map((node) => {
          if (node.kind === "folder") {
            const open = !closed.has(node.path);
            return (
              <li key={node.path}>
                <ListRow
                  type="button"
                  font="mono"
                  depth={depth}
                  data-tree-path={node.path}
                  tabIndex={-1}
                  title={node.path}
                  aria-expanded={open}
                  aria-label={`${open ? "Collapse" : "Expand"} folder ${node.path}`}
                  onClick={() => onToggle(node.path)}
                  onKeyDown={onKeyDown}
                >
                  <span className="inline-flex text-muted">
                    <DisclosureIcon open={open} />
                  </span>
                  <FolderIcon open={open} />
                  <span className="min-w-0 flex-1 truncate">{node.name}</span>
                </ListRow>
                {open && renderNodes(node.children, depth + 1)}
              </li>
            );
          }
          const file = node.file;
          const viewed = isReviewed(file);
          const decoration = gitDecoration(file);
          const { current: count, outdated } = commentCounts.get(file.path) ?? {
            current: 0,
            outdated: 0,
          };
          return (
            <li key={node.path}>
              <ListRow
                type="button"
                font="mono"
                depth={depth}
                data-path={file.path}
                data-tree-path={file.path}
                tabIndex={selected === file.path ? 0 : -1}
                aria-current={selected === file.path}
                aria-label={`${file.path}, ${decoration.label}${viewed ? ", reviewed" : ""}${count ? `, ${count} review ${count === 1 ? "comment" : "comments"}` : ""}${outdated ? `, ${outdated} outdated` : ""}`}
                title={`${file.oldPath ? `${file.oldPath} → ${file.path}` : file.path}\n${decoration.details}`}
                onClick={() => onSelect(file.path)}
                onKeyDown={onKeyDown}
              >
                <FileIcon path={file.path} reviewed={viewed} />
                <span className="min-w-0 flex-1 truncate">{node.name}</span>
                {count > 0 && (
                  <span
                    className="file-comment-count"
                    title={`${count} review ${count === 1 ? "comment" : "comments"}`}
                  >
                    <CommentIcon /> {count}
                  </span>
                )}
                {outdated > 0 && (
                  <span
                    className="file-comment-count outdated"
                    title={`${outdated} outdated review ${outdated === 1 ? "comment" : "comments"}`}
                  >
                    <CommentIcon /> {outdated}
                  </span>
                )}
                <StatusBadge file={file} stagingMetadata={stagingMetadata} />
              </ListRow>
            </li>
          );
        })}
      </ul>
    );
  }
  return (
    <nav
      id="file-tree"
      className="file-tree"
      aria-label="Changed file navigation"
    >
      {files.length ? (
        renderNodes(buildFileTree(files), 0)
      ) : (
        <EmptyState heading="p" title={emptyMessage} />
      )}
    </nav>
  );
}
