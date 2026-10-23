import type { ApiContext } from "@diffx/api";
import { compareContextActivity, contextActivity } from "./context-activity.ts";
import {
  contextHasChanges,
  contextIsPiped,
  contextTimestamp,
} from "./project-picker.ts";

// The projects drawer shows only current work: each stream's head with changes.
// Stale snapshots stay reachable through the review picker (Cmd/Ctrl+K).

export type ProjectStream = {
  id: string;
  context: ApiContext;
  unseen: boolean;
};

export type ProjectGroup = {
  id: string;
  name: string;
  kind: "repository" | "piped";
  streams: ProjectStream[];
  lastActivity: number;
  multipleHosts: boolean;
};

export const pipedProjectId = "piped";
const pipedLimit = 5;

/** Saved per browser: stream ID to the newest context the user has opened. */
export type SeenStreams = Record<string, string>;

export function streamKey(context: ApiContext) {
  return context.streamId ?? context.id;
}

export function projectKey(context: ApiContext) {
  if (contextIsPiped(context)) return pipedProjectId;
  return context.repositoryId ?? `name:${context.name}`;
}

export function projectGroups(heads: ApiContext[], seen: SeenStreams | null) {
  const groups = new Map<string, ProjectGroup>();
  for (const context of heads) {
    if (context.stale || !contextHasChanges(context)) continue;
    const id = projectKey(context);
    const piped = id === pipedProjectId;
    const group = groups.get(id) ?? {
      id,
      name: piped ? "Piped" : context.name,
      kind: piped ? "piped" : "repository",
      streams: [],
      lastActivity: 0,
      multipleHosts: false,
    };
    const stream = streamKey(context);
    group.streams.push({
      id: stream,
      context,
      unseen: seen !== null && seen[stream] !== context.id,
    });
    group.lastActivity = Math.max(group.lastActivity, contextActivity(context));
    groups.set(id, group);
  }
  const result = [...groups.values()];
  for (const group of result) {
    group.streams.sort((left, right) =>
      group.kind === "piped"
        ? contextTimestamp(right.context) - contextTimestamp(left.context) ||
          left.id.localeCompare(right.id)
        : compareContextActivity(left.context, right.context),
    );
    if (group.kind === "piped")
      group.streams = group.streams.slice(0, pipedLimit);
    group.multipleHosts =
      new Set(group.streams.map((stream) => streamHost(stream.context))).size >
      1;
  }
  return result.sort((left, right) => {
    const leftHead = left.streams[0]?.context;
    const rightHead = right.streams[0]?.context;
    return (
      Number(left.kind === "piped") - Number(right.kind === "piped") ||
      (leftHead && rightHead
        ? compareContextActivity(leftHead, rightHead)
        : 0) ||
      left.id.localeCompare(right.id)
    );
  });
}

export function streamHost(context: ApiContext) {
  return context.observation?.hostname ?? context.observation?.sourceId ?? "";
}

/** The review column lists streams grouped by repository or as one recent list. */
export type StreamGroupMode = "repo" | "recent";

export type RecentStream = ProjectStream & { group: ProjectGroup };

/**
 * Every visible stream across repositories, most active first, so hopping
 * between agents ignores repository boundaries. Piped keeps its capture order.
 */
export function recentStreams(groups: ProjectGroup[]): RecentStream[] {
  return groups
    .flatMap((group) => group.streams.map((stream) => ({ ...stream, group })))
    .sort(
      (left, right) =>
        Number(left.group.kind === "piped") -
          Number(right.group.kind === "piped") ||
        (left.group.kind === "piped"
          ? 0
          : compareContextActivity(left.context, right.context)),
    );
}

/** Persisted inbox facets; an empty facet matches every stream. */
export type StreamFilters = {
  repos: string[];
  sessions: string[];
  hosts: string[];
  worktrees: string[];
  unread: boolean;
};

export const defaultStreamFilters: StreamFilters = {
  repos: [],
  sessions: [],
  hosts: [],
  worktrees: [],
  unread: false,
};

export function streamWorktree(context: ApiContext) {
  return context.observation?.linkedWorktree
    ? (context.worktreeName ?? context.observation.worktreeName ?? "")
    : "";
}

/** Agent sessions that touched the stream, labeled by name and harness. */
export function streamSessions(context: ApiContext) {
  const sessions = [
    ...(context.sessions ?? []),
    ...(context.observation?.agentSession
      ? [context.observation.agentSession]
      : []),
  ];
  return [
    ...new Set(
      sessions.map(({ harness, name }) =>
        name ? `${name} (${harness})` : harness,
      ),
    ),
  ];
}

export function streamFacets(groups: ProjectGroup[]) {
  const streams = groups.flatMap((group) => group.streams);
  const values = (pick: (context: ApiContext) => string[]) =>
    [...new Set(streams.flatMap((stream) => pick(stream.context)))].sort(
      (left, right) => left.localeCompare(right),
    );
  return {
    repos: [...new Set(groups.map((group) => group.name))],
    sessions: values(streamSessions),
    hosts: values((context) => [streamHost(context)].filter(Boolean)),
    worktrees: values((context) => [streamWorktree(context)].filter(Boolean)),
  };
}

export function streamFiltersChanged(filters: StreamFilters) {
  return (
    filters.unread ||
    filters.repos.length > 0 ||
    filters.sessions.length > 0 ||
    filters.hosts.length > 0 ||
    filters.worktrees.length > 0
  );
}

/** Keeps groups and order; drops streams outside the facets or text query. */
export function filterStreams(
  groups: ProjectGroup[],
  filters: StreamFilters,
  query: string,
): ProjectGroup[] {
  const terms = query.trim().toLowerCase().split(/\s+/).filter(Boolean);
  const matches = (selected: string[], values: string[]) =>
    !selected.length || values.some((value) => selected.includes(value));
  return groups
    .filter((group) => matches(filters.repos, [group.name]))
    .map((group) => ({
      ...group,
      streams: group.streams.filter(({ context, unseen }) => {
        if (filters.unread && !unseen) return false;
        const sessions = streamSessions(context);
        const host = streamHost(context);
        const worktree = streamWorktree(context);
        if (!matches(filters.sessions, sessions)) return false;
        if (!matches(filters.hosts, [host])) return false;
        if (!matches(filters.worktrees, [worktree])) return false;
        const text = [group.name, streamTitle(context), worktree, host]
          .concat(sessions)
          .join(" ")
          .toLowerCase();
        return terms.every((term) => text.includes(term));
      }),
    }))
    .filter((group) => group.streams.length > 0);
}

export function readStreamFilters(raw: string | null): StreamFilters {
  try {
    const data: unknown = JSON.parse(raw ?? "{}");
    if (typeof data !== "object" || data === null || Array.isArray(data))
      return defaultStreamFilters;
    const strings = (key: string) => {
      const value: unknown = Reflect.get(data, key);
      return Array.isArray(value)
        ? value.filter((item) => typeof item === "string")
        : [];
    };
    return {
      repos: strings("repos"),
      sessions: strings("sessions"),
      hosts: strings("hosts"),
      worktrees: strings("worktrees"),
      unread: Reflect.get(data, "unread") === true,
    };
  } catch {
    return defaultStreamFilters;
  }
}

/** One or two letters marking a repository in the collapsed review rail. */
export function repoMonogram(name: string) {
  const words = name
    .replace(/([a-z])([A-Z])/g, "$1 $2")
    .split(/[^\p{L}\p{N}]+/u)
    .filter(Boolean);
  const [first = "", second = ""] = words;
  if (second) return `${first.charAt(0)}${second.charAt(0)}`.toUpperCase();
  return first.charAt(0).toUpperCase() + first.slice(1, 2).toLowerCase();
}

export function streamTitle(context: ApiContext) {
  if (contextIsPiped(context)) return "Piped diff";
  return context.branch ?? context.observation?.branch ?? "Detached HEAD";
}

export function streamMeta(
  context: ApiContext,
  multipleHosts: boolean,
  now: number,
) {
  const worktree = streamWorktree(context);
  const harness =
    [...(context.sessions ?? [])].sort(
      (left, right) => right.lastObservedAt - left.lastObservedAt,
    )[0]?.harness ??
    context.observation?.agentSession?.harness ??
    "";
  return [
    worktree,
    harness,
    multipleHosts ? streamHost(context) : "",
    relativeAge(contextActivity(context), now),
  ]
    .filter(Boolean)
    .join(" · ");
}

export function relativeAge(timestamp: number, now: number) {
  const minutes = Math.floor(Math.max(0, now - timestamp) / 60_000);
  if (minutes < 1) return "now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

/** First visit marks current work as seen so only later arrivals stand out. */
export function baselineSeen(heads: ApiContext[]): SeenStreams {
  return Object.fromEntries(
    heads.map((context) => [streamKey(context), context.id]),
  );
}

/** Unread content heads, independent of ingestion order and submission touches. */
export function unseenChangedHeads(
  heads: ApiContext[],
  seen: SeenStreams | null,
) {
  if (seen === null) return [];
  return heads
    .filter(
      (context) =>
        !context.stale &&
        contextHasChanges(context) &&
        seen[streamKey(context)] !== context.id,
    )
    .sort(compareContextActivity);
}

export function readSeen(raw: string | null): SeenStreams | null {
  if (raw === null) return null;
  try {
    const data: unknown = JSON.parse(raw);
    if (typeof data !== "object" || data === null || Array.isArray(data))
      return null;
    const seen: SeenStreams = {};
    for (const [key, value] of Object.entries(data))
      if (typeof value === "string") seen[key] = value;
    return seen;
  } catch {
    return null;
  }
}

/** The head that supersedes the viewed snapshot, if any. */
export function newerHead(current: ApiContext, heads: ApiContext[]) {
  if (!current.streamId) return undefined;
  return heads.find(
    (context) =>
      context.streamId === current.streamId && context.id !== current.id,
  );
}
