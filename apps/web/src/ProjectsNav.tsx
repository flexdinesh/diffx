import type { ApiContext } from "@diffx/api";
import {
  ChevronRightIcon,
  FolderGit2Icon,
  GitBranchIcon,
  HistoryIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  SearchIcon,
  SquareTerminalIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { save, saved } from "./preferences.ts";
import { contextChangeLabel, contextIsPiped } from "./project-picker.ts";
import {
  baselineSeen,
  newerHead,
  projectGroups,
  readSeen,
  relativeAge,
  streamKey,
  streamMeta,
  streamTitle,
  type ProjectGroup,
  type SeenStreams,
} from "./project-nav.ts";
import { useProjectsNavState } from "./projects-nav-context.tsx";
import { contextPath, useCatalog, useSession } from "./session-context.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

const shortcut = navigator.platform.includes("Mac") ? "⌘K" : "Ctrl K";

/** Topbar start while the drawer is hidden: show-projects toggle and brand. */
export function ProjectsToggle() {
  const projects = useProjectsNavState();
  if (projects.expanded) return null;
  return (
    <>
      <Button
        type="button"
        id="projects-toggle"
        variant="ghost"
        size="icon"
        aria-controls="projects-nav"
        aria-expanded={false}
        aria-label="Show projects"
        title="Show projects"
        onClick={projects.toggle}
      >
        <PanelLeftOpenIcon className="size-(--icon-base)" />
      </Button>
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

// Seen state is per browser: the first visit marks existing work as seen, and
// opening a stream's newest snapshot clears its new-changes mark.
function useSeenStreams(heads: ApiContext[], loaded: boolean) {
  const { current } = useCatalog();
  const [seen, setSeen] = useState<SeenStreams | null>(() =>
    readSeen(saved("projects-seen")),
  );
  useEffect(() => {
    if (seen === null && loaded) setSeen(baselineSeen(heads));
  }, [seen, loaded, heads]);
  const currentHead =
    current && heads.some((context) => context.id === current.id)
      ? current
      : undefined;
  useEffect(() => {
    if (!currentHead) return;
    const stream = streamKey(currentHead);
    setSeen((value) =>
      !value || value[stream] === currentHead.id
        ? value
        : { ...value, [stream]: currentHead.id },
    );
  }, [currentHead]);
  useEffect(() => {
    if (seen) save("projects-seen", JSON.stringify(seen));
  }, [seen]);
  return seen;
}

export function ProjectsNav() {
  const projects = useProjectsNavState();
  const sidebar = useSidebarState();
  const { heads, current, select, openPicker } = useCatalog();
  const seen = useSeenStreams(heads.contexts, heads.loaded);
  const [closed, setClosed] = useState(readCollapsedGroups);
  const now = useNow();
  const groups = projectGroups(heads.contexts, seen);
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
  const open = (event: MouseEvent<HTMLAnchorElement>, id: string) => {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
      return;
    event.preventDefault();
    select(id, { restoreFocus: false });
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
          <a className="brand" href="/" aria-label="diffx home">
            diffx
          </a>
          <Button
            type="button"
            id="projects-hide"
            variant="ghost"
            size="icon"
            aria-controls="projects-nav"
            aria-label="Hide projects"
            title="Hide projects"
            onClick={projects.toggle}
          >
            <PanelLeftCloseIcon className="size-(--icon-base)" />
          </Button>
        </div>
        <button
          type="button"
          className="projects-search"
          onClick={() => {
            projects.closeMobile();
            openPicker();
          }}
        >
          <SearchIcon className="size-(--icon-sm)" aria-hidden="true" />
          <span>Search reviews</span>
          <kbd>{shortcut}</kbd>
        </button>
        <h2 className="projects-heading">Projects</h2>
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
  onOpen: (event: MouseEvent<HTMLAnchorElement>, id: string) => void;
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
                href={contextPath(context.id)}
                aria-current={id === currentStream ? "page" : undefined}
                title={`${streamTitle(context)} · ${contextChangeLabel(context)}`}
                onClick={(event) => onOpen(event, context.id)}
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
  error,
  onRetry,
}: {
  loaded: boolean;
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
    <div className="projects-empty" role="status">
      <p>{loaded ? "No current changes" : "Loading projects…"}</p>
      {loaded && (
        <p className="projects-empty-hint">
          Agent hooks and diffx sync publish changed checkouts here.
        </p>
      )}
    </div>
  );
}

/** Announces a newer head for the viewed stream without replacing the review. */
export function NewerSnapshotNotice() {
  const session = useSession();
  const { heads, select } = useCatalog();
  const newer = newerHead(session, heads.contexts);
  const now = useNow();
  return (
    <div className="snapshot-notice" role="status" hidden={!newer}>
      {newer && (
        <>
          <HistoryIcon className="size-(--icon-sm)" aria-hidden="true" />
          <p>
            Newer snapshot of {streamTitle(newer)} · {contextChangeLabel(newer)}{" "}
            · {relativeAge(newer.lastSubmittedAt, now)}
          </p>
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={() => select(newer.id, { restoreFocus: false })}
          >
            View latest
          </Button>
        </>
      )}
    </div>
  );
}
