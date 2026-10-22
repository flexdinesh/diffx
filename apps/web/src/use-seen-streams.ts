import type { ApiContext } from "@diffx/api";
import { useEffect, useState } from "react";
import { save, saved } from "./preferences.ts";
import {
  baselineSeen,
  readSeen,
  streamKey,
  type SeenStreams,
} from "./project-nav.ts";

// One browser-local owner supplies both drawer dots and global navigation.
// Each account baselines its own heads; local mode keeps the original key.
export function useSeenStreams(
  account: string | null,
  heads: ApiContext[],
  loaded: boolean,
  current: ApiContext | undefined,
) {
  const key = account === null ? "projects-seen" : `projects-seen:${account}`;
  const [seen, setSeen] = useState<SeenStreams | null>(() =>
    readSeen(saved(key)),
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
    if (seen) save(key, JSON.stringify(seen));
  }, [key, seen]);
  return seen;
}
