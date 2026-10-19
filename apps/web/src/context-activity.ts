import type { ApiContext } from "@diffx/api";
import { contextHasChanges } from "./project-picker.ts";

/** Advisory code activity, falling back to the snapshot's captured time. */
export function contextActivity(context: ApiContext) {
  return context.lastChangedAt > 0
    ? context.lastChangedAt
    : (context.observation?.collectedAt ?? context.createdAt);
}

export function compareContextActivity(left: ApiContext, right: ApiContext) {
  return (
    contextActivity(right) - contextActivity(left) ||
    (left.streamId ?? left.id).localeCompare(right.streamId ?? right.id) ||
    left.id.localeCompare(right.id)
  );
}

export function latestChangedContext(contexts: ApiContext[]) {
  return contexts
    .filter((context) => !context.stale && contextHasChanges(context))
    .sort(compareContextActivity)[0];
}
