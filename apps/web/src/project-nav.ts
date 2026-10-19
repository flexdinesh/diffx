import type { ApiContext } from "@diffx/api";
import { compareContextActivity, contextActivity } from "./context-activity.ts";
import {
  contextHasChanges,
  contextIsPiped,
  contextMatches,
  contextTimestamp,
  searchWords,
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

/** Keeps streams whose metadata matches every search word, preserving order. */
export function searchProjectGroups(groups: ProjectGroup[], query: string) {
  const words = searchWords(query);
  if (!words.length) return groups;
  return groups
    .map((group) => ({
      ...group,
      streams: group.streams.filter((stream) =>
        contextMatches(stream.context, words).every((score) => score > 0),
      ),
    }))
    .filter((group) => group.streams.length);
}

function streamHost(context: ApiContext) {
  return context.observation?.hostname ?? context.observation?.sourceId ?? "";
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
  const worktree = context.observation?.linkedWorktree
    ? (context.worktreeName ?? context.observation.worktreeName)
    : "";
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
