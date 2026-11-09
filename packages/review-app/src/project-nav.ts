import type { ApiContext } from "@diffx/api";
import {
  compareContextActivity,
  contextActivity,
  latestChangedContext,
} from "./context-activity.ts";
import { contextHasChanges, searchWords } from "./project-picker.ts";

// The header comboboxes list each stream's head with changes in current work;
// the Review bench (review-bench.ts) lists every state. Stale snapshots stay
// reachable through the review picker (Cmd/Ctrl+K).

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

/** The GitHub pull request a stream reviews; the server alone authors it. */
export function pullRequestOf(context: ApiContext) {
  return context.observation?.pullRequest;
}

/** Closed and merged pull requests leave current work; their reviews remain. */
function currentWork(context: ApiContext) {
  const state = pullRequestOf(context)?.state;
  return state !== "closed" && state !== "merged";
}

/** A titled run of one repository's streams in the header branch combobox. */
export type StreamSection = {
  value: "Pull requests" | "From your machines";
  items: ProjectStream[];
};

/**
 * The branch combobox's rows for one repository: grouped under "Pull
 * requests" and "From your machines" only when both list streams.
 */
export function branchSections(
  streams: ProjectStream[],
): StreamSection[] | null {
  const pulls = streams.filter((stream) => pullRequestOf(stream.context));
  const machines = streams.filter((stream) => !pullRequestOf(stream.context));
  if (!pulls.length || !machines.length) return null;
  return [
    { value: "Pull requests", items: pulls },
    { value: "From your machines", items: machines },
  ];
}

/** Repositories and their current streams: heads with changes, unread marked. */
export function projectGroups(
  heads: ApiContext[],
  unread: (context: ApiContext) => boolean,
) {
  const groups = new Map<string, ProjectGroup>();
  for (const context of heads) {
    if (context.stale || !contextHasChanges(context) || !currentWork(context))
      continue;
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
      unseen: unread(context),
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

/** The stream the header repository combobox opens: its most active one. */
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

/**
 * Every query word appears in the stream's branch, worktree, host, sessions
 * or `extra` text, such as its repository name on the bench.
 */
export function streamMatches(
  stream: ProjectStream,
  query: string,
  extra = "",
) {
  const { context } = stream;
  const text = [
    extra,
    streamTitle(context),
    context.branch ?? "",
    pullRequestOf(context)?.author.login ?? "",
    streamWorktree(context),
    streamHost(context),
    ...streamSessions(context),
  ]
    .join(" ")
    .toLowerCase();
  return searchWords(query).every((word) => text.includes(word));
}

export function streamTitle(context: ApiContext) {
  const pull = pullRequestOf(context);
  if (pull) return `#${pull.number} ${pull.title}`;
  return context.branch ?? context.observation?.branch ?? "Detached HEAD";
}

/** The agent harness that last touched the stream, or empty. */
export function streamHarness(context: ApiContext) {
  return (
    [...(context.sessions ?? [])].sort(
      (left, right) => right.lastObservedAt - left.lastObservedAt,
    )[0]?.harness ??
    context.observation?.agentSession?.harness ??
    ""
  );
}

export function streamMeta(
  context: ApiContext,
  multipleHosts: boolean,
  now: number,
) {
  const worktree = streamWorktree(context);
  const harness = streamHarness(context);
  const pull = pullRequestOf(context);
  return [
    pull ? "GitHub" : "",
    pull?.state === "merged" ? "Merged" : "",
    pull?.state === "closed" ? "Closed" : "",
    pull?.draft ? "Draft" : "",
    pull ? `@${pull.author.login}` : "",
    worktree,
    harness,
    multipleHosts && !pull ? streamHost(context) : "",
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

/**
 * The review a capture's root `/` opens: the most active current work,
 * falling back to a closed or merged pull request only when nothing current
 * changed. Review navigation shows the Review bench at `/` instead.
 */
export function defaultContext(contexts: ApiContext[]) {
  return (
    latestChangedContext(contexts.filter(currentWork)) ??
    latestChangedContext(contexts)
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
