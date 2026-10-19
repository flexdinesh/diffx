import type { ApiContext } from "@diffx/api";
import {
  ChevronRightIcon,
  FolderGit2Icon,
  GitBranchIcon,
  HistoryIcon,
  ListIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  SearchIcon,
  SquareTerminalIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { save, saved } from "./preferences.ts";
import { contextChangeLabel, contextIsPiped } from "./project-picker.ts";
import { contextActivity } from "./context-activity.ts";
import {
  newerHead,
  projectGroups,
  relativeAge,
  searchProjectGroups,
  streamKey,
  streamMeta,
  streamTitle,
  type ProjectGroup,
} from "./project-nav.ts";
import { useProjectsNavState } from "./projects-nav-context.tsx";
import { streamPath, useCatalog, useSession } from "./session-context.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

const shortcut = navigator.platform.includes("Mac") ? "⌘K" : "Ctrl K";

/** Topbar start: projects toggle above the activity bar, brand and divider. */
export function ProjectsToggle() {
  const projects = useProjectsNavState();
  const action = projects.expanded ? "Hide projects" : "Show projects";
  const Icon = projects.expanded ? PanelLeftCloseIcon : PanelLeftOpenIcon;
  return (
    <>
      <div className="topbar-lead">
        <Button
          type="button"
          id="projects-toggle"
          variant="ghost"
          size="icon"
          aria-controls="projects-nav"
          aria-expanded={projects.expanded}
          aria-label={action}
          title={action}
          onClick={projects.toggle}
        >
          <Icon className="size-(--icon-base)" />
        </Button>
      </div>
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
  const projects = useProjectsNavState();
  const sidebar = useSidebarState();
  const { heads, current, openStream, openPicker, seen, collecting } =
    useCatalog();
  const [closed, setClosed] = useState(readCollapsedGroups);
  const [query, setQuery] = useState("");
  const now = useNow();
  const searching = query.trim() !== "";
  const allGroups = projectGroups(heads.contexts, seen);
  const groups = searchProjectGroups(allGroups, query);
  const currentStream = current ? streamKey(current) : "";
  const { closeMobile: closeFiles } = sidebar;
  useEffect(() => {
    if (projects.open) closeFiles();
  }, [projects.open, closeFiles]);
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
    openStream(head, { restoreFocus: false });
    projects.closeMobile();
  };
  return (
    <>
      {projects.mobile && projects.open && (
        <button
          type="button"
          className="sidebar-backdrop"
          aria-label="Close projects"
          tabIndex={-1}
          onClick={projects.closeMobile}
        />
      )}
      <nav
        id="projects-nav"
        className={projects.open ? "projects-nav open" : "projects-nav"}
        aria-label="Projects"
        hidden={!projects.expanded}
      >
        <div className="projects-nav-header">
          <h2 className="projects-heading">Projects</h2>
          <Button
            type="button"
            id="projects-browse"
            variant="ghost"
            size="icon"
            aria-label="Browse all reviews"
            title={`Browse all reviews (${shortcut})`}
            onClick={() => {
              projects.closeMobile();
              openPicker();
            }}
          >
            <ListIcon aria-hidden="true" />
          </Button>
          <Button
            type="button"
            className="mobile-only"
            variant="ghost"
            size="icon"
            aria-label="Close projects"
            onClick={projects.closeMobile}
          >
            <XIcon aria-hidden="true" />
          </Button>
        </div>
        <div className="search-box projects-search">
          <SearchIcon className="size-(--icon-sm)" aria-hidden="true" />
          <Input
            id="projects-search"
            type="search"
            placeholder="Search projects…"
            aria-label="Search projects"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            onKeyDown={(event) => {
              if (event.key !== "Escape" || !query) return;
              event.stopPropagation();
              setQuery("");
            }}
          />
        </div>
        <p className="visually-hidden" role="status" aria-live="polite">
          {searching
            ? `${groups.reduce((sum, group) => sum + group.streams.length, 0)} matching streams`
            : ""}
        </p>
        <div className="projects-scroll">
          {groups.length ? (
            <ul className="projects-list">
              {groups.map((group) => (
                <ProjectItem
                  key={group.id}
                  group={group}
                  expanded={searching || !closed.has(group.id)}
                  onToggle={() => toggleGroup(group.id)}
                  currentStream={currentStream}
                  now={now}
                  onOpen={open}
                />
              ))}
            </ul>
          ) : searching && allGroups.length ? (
            <div className="projects-empty" role="status">
              <p>No matching projects</p>
            </div>
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
