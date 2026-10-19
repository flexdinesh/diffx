import type { ApiContext } from "@diffx/api";
import {
  ChevronRightIcon,
  FolderGit2Icon,
  GitBranchIcon,
  HistoryIcon,
  SearchIcon,
  SquareTerminalIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { save, saved } from "./preferences.ts";
import { contextChangeLabel, contextIsPiped } from "./project-picker.ts";
import { contextActivity } from "./context-activity.ts";
import {
  newerHead,
  projectGroups,
  relativeAge,
  streamKey,
  streamMeta,
  streamTitle,
  type ProjectGroup,
} from "./project-nav.ts";
import { streamPath, useCatalog, useSession } from "./session-context.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

const shortcut = navigator.platform.includes("Mac") ? "⌘K" : "Ctrl K";

/** Compact entry point; desktop view controls live in the activity rail. */
export function ProjectsToggle() {
  const sidebar = useSidebarState();
  const expanded = sidebar.view === "projects" && sidebar.expanded;
  const action = expanded ? "Hide projects" : "Show projects";
  return (
    <div className="topbar-lead">
      <Button
        type="button"
        id="projects-toggle"
        variant="ghost"
        size="icon"
        aria-controls="projects-nav"
        aria-expanded={expanded}
        aria-label={action}
        title={action}
        onClick={() => sidebar.selectView("projects")}
      >
        <FolderGit2Icon className="size-(--icon-base)" />
      </Button>
    </div>
  );
}

export function HeaderBrand() {
  return (
    <>
      <a className="brand" href="/" aria-label="diffx home">
        diffx
      </a>
      <Separator className="header-divider" orientation="vertical" />
    </>
  );
}

function useNow() {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

function readCollapsedGroups() {
  try {
    const data: unknown = JSON.parse(saved("projects-closed-groups") ?? "[]");
    return new Set(
      Array.isArray(data)
        ? data.filter((value) => typeof value === "string")
        : [],
    );
  } catch {
    return new Set<string>();
  }
}

export function ProjectsNav() {
  const sidebar = useSidebarState();
  const { heads, current, openStream, openPicker, seen, collecting } =
    useCatalog();
  const [closed, setClosed] = useState(readCollapsedGroups);
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen);
  const currentStream = current ? streamKey(current) : "";
  const expanded = sidebar.view === "projects" && sidebar.expanded;
  useEffect(() => {
    save("projects-closed-groups", JSON.stringify([...closed]));
  }, [closed]);
  const toggleGroup = (id: string) =>
    setClosed((value) => {
      const next = new Set(value);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  const open = (event: MouseEvent<HTMLAnchorElement>, head: ApiContext) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
      return;
    event.preventDefault();
    openStream(head);
  };
  return (
    <>
      {sidebar.mobile && sidebar.open && sidebar.view === "projects" && (
        <button
          type="button"
          className="sidebar-backdrop"
          aria-label="Close projects"
          tabIndex={-1}
          onClick={sidebar.closeMobile}
        />
      )}
      <nav
        id="projects-nav"
        className={
          sidebar.open && sidebar.view === "projects"
            ? "projects-nav open"
            : "projects-nav"
        }
        aria-label="Projects"
        role={sidebar.mobile && expanded ? "dialog" : undefined}
        aria-modal={sidebar.mobile && expanded ? "true" : undefined}
        hidden={!expanded}
      >
        <div className="projects-nav-header">
          <h2 className="projects-heading">Projects</h2>
          <Button
            type="button"
            id="projects-browse"
            variant="ghost"
            size="icon"
            aria-label="Search reviews"
            title={`Search reviews (${shortcut})`}
            onClick={() => {
              sidebar.closeMobile();
              openPicker();
            }}
          >
            <SearchIcon aria-hidden="true" />
          </Button>
          <Button
            type="button"
            className="mobile-only"
            variant="ghost"
            size="icon"
            aria-label="Close projects"
            onClick={sidebar.closeMobile}
          >
            <XIcon aria-hidden="true" />
          </Button>
        </div>
        <div className="projects-scroll">
          {groups.length ? (
            <ul className="projects-list">
              {groups.map((group) => (
                <ProjectItem
                  key={group.id}
                  group={group}
                  expanded={!closed.has(group.id)}
                  onToggle={() => toggleGroup(group.id)}
                  currentStream={currentStream}
                  now={now}
                  onOpen={open}
                />
              ))}
            </ul>
          ) : (
            <ProjectsEmpty
              loaded={heads.loaded}
              collecting={collecting}
              error={heads.error}
              onRetry={heads.retry}
            />
          )}
        </div>
      </nav>
    </>
  );
}

function ProjectItem({
  group,
  expanded,
  onToggle,
  currentStream,
  now,
  onOpen,
}: {
  group: ProjectGroup;
  expanded: boolean;
  onToggle: () => void;
  currentStream: string;
  now: number;
  onOpen: (event: MouseEvent<HTMLAnchorElement>, head: ApiContext) => void;
}) {
  const unseen = group.streams.some((stream) => stream.unseen);
  const Icon = group.kind === "piped" ? SquareTerminalIcon : FolderGit2Icon;
  const listId = `project-${group.id}`;
  return (
    <li>
      <button
        type="button"
        className="project-row"
        aria-expanded={expanded}
        aria-controls={listId}
        title={group.name}
        onClick={onToggle}
      >
        <ChevronRightIcon
          className="project-chevron size-(--icon-sm)"
          aria-hidden="true"
        />
        <Icon className="size-(--icon-sm)" aria-hidden="true" />
        <span className="project-name">{group.name}</span>
        {unseen && !expanded && <NewMark />}
        <span className="project-age">
          {relativeAge(group.lastActivity, now)}
        </span>
      </button>
      {expanded && (
        <ul id={listId} className="stream-list">
          {group.streams.map(({ id, context, unseen: fresh }) => (
            <li key={id}>
              <a
                className="stream-row"
                href={streamPath(context)}
                aria-current={id === currentStream ? "page" : undefined}
                title={`${streamTitle(context)} · ${contextChangeLabel(context)}`}
                onClick={(event) => onOpen(event, context)}
              >
                <span className="stream-title">
                  {!contextIsPiped(context) && (
                    <GitBranchIcon
                      className="size-(--icon-sm)"
                      aria-hidden="true"
                    />
                  )}
                  <span className="stream-name">{streamTitle(context)}</span>
                  {fresh && id !== currentStream && <NewMark />}
                  <span
                    className="stream-count"
                    aria-label={contextChangeLabel(context)}
                  >
                    {context.changedFileCount}
                  </span>
                </span>
                <span className="stream-meta">
                  {streamMeta(context, group.multipleHosts, now)}
                </span>
              </a>
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

function NewMark() {
  return (
    <span className="new-mark" role="img" aria-label="New changes">
      <span aria-hidden="true" />
    </span>
  );
}

function ProjectsEmpty({
  loaded,
  collecting,
  error,
  onRetry,
}: {
  loaded: boolean;
  collecting: boolean;
  error: string;
  onRetry: () => void;
}) {
  if (error)
    return (
      <div className="projects-empty" role="alert">
        <p>{error}</p>
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          Retry
        </Button>
      </div>
    );
  return (
    <div
      className="projects-empty"
      role="status"
      aria-busy={collecting || !loaded}
    >
      <p>
        {collecting
          ? "Collecting projects…"
          : loaded
            ? "No current changes"
            : "Loading projects…"}
      </p>
      {loaded && !collecting && (
        <p className="projects-empty-hint">
          Agent hooks and diffx sync publish changed checkouts here.
        </p>
      )}
    </div>
  );
}

/** Opens the most active unread head without interrupting the open review. */
export function NewChangesButton() {
  const { unreadHeads, openStream } = useCatalog();
  const latest = unreadHeads[0];
  if (!latest) return null;
  const count = unreadHeads.length;
  return (
    <Button
      type="button"
      id="new-changes"
      variant="ghost"
      size="sm"
      aria-label={`Open latest new changes (${count} ${count === 1 ? "stream" : "streams"})`}
      title={`Open ${latest.name} · ${streamTitle(latest)}`}
      onClick={() => openStream(latest)}
    >
      <HistoryIcon aria-hidden="true" />
      <span>
        {count}
        <span className="max-md:hidden"> new</span>
      </span>
    </Button>
  );
}

/** Announces a newer head for the viewed stream without replacing the review. */
export function NewerSnapshotNotice() {
  const session = useSession();
  const { heads, openStream } = useCatalog();
  const newer = newerHead(session, heads.contexts);
  const now = useNow();
  return (
    <div className="snapshot-notice" role="status" hidden={!newer}>
      {newer && (
        <>
          <HistoryIcon className="size-(--icon-sm)" aria-hidden="true" />
          <p>
            Newer snapshot of {streamTitle(newer)} · {contextChangeLabel(newer)}{" "}
            · {relativeAge(contextActivity(newer), now)}
          </p>
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={() => openStream(newer, { restoreFocus: false })}
          >
            View latest
          </Button>
        </>
      )}
    </div>
  );
}
