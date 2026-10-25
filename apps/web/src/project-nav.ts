import type { ApiContext } from "@diffx/api";
import { compareContextActivity, contextActivity } from "./context-activity.ts";
import { contextHasChanges, searchWords } from "./project-picker.ts";

// The review column shows only current work: each stream's head with changes.
// Stale snapshots stay reachable through the review picker (Cmd/Ctrl+K).

export type ProjectStream = {
  id: string;
  context: ApiContext;
  unseen: boolean;
};

export type ProjectGroup = {
  id: string;
  name: string;
  streams: ProjectStream[];
  lastActivity: number;
  multipleHosts: boolean;
};

/** Saved per browser: stream ID to the newest context the user has opened. */
export type SeenStreams = Record<string, string>;

export function streamKey(context: ApiContext) {
  return context.streamId ?? context.id;
}

export function projectKey(context: ApiContext) {
  return context.repositoryId ?? `name:${context.name}`;
}

export function projectGroups(heads: ApiContext[], seen: SeenStreams | null) {
  const groups = new Map<string, ProjectGroup>();
  for (const context of heads) {
    if (context.stale || !contextHasChanges(context)) continue;
    const id = projectKey(context);
    const group = groups.get(id) ?? {
      id,
      name: context.name,
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
      compareContextActivity(left.context, right.context),
    );
    group.multipleHosts =
      new Set(group.streams.map((stream) => streamHost(stream.context))).size >
      1;
  }
  return result.sort((left, right) => {
    const leftHead = left.streams[0]?.context;
    const rightHead = right.streams[0]?.context;
    return (
      (leftHead && rightHead
        ? compareContextActivity(leftHead, rightHead)
        : 0) || left.id.localeCompare(right.id)
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
 * between agents ignores repository boundaries.
 */
export function recentStreams(groups: ProjectGroup[]): RecentStream[] {
  return groups
    .flatMap((group) => group.streams.map((stream) => ({ ...stream, group })))
    .sort((left, right) => compareContextActivity(left.context, right.context));
}

/**
 * The stream a repository opens to from the rail, its name in the column, or
 * the header repository combobox: its most active current stream.
 */
export function latestStream(group: ProjectGroup): ProjectStream | undefined {
  return group.streams[0];
}

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

/** Every query word appears in the stream's branch, worktree, host or sessions. */
export function streamMatches(stream: ProjectStream, query: string) {
  const { context } = stream;
  const text = [
    streamTitle(context),
    streamWorktree(context),
    streamHost(context),
    ...streamSessions(context),
  ]
    .join(" ")
    .toLowerCase();
  return searchWords(query).every((word) => text.includes(word));
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
