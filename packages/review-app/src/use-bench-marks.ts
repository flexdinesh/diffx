import type { ApiContext } from "@diffx/api";
import { useCallback, useEffect, useMemo, useState } from "react";
import { save, saved } from "@diffx/ui/lib/storage";
import { contextActivity } from "./context-activity.ts";
import { readSeen, streamKey, type SeenStreams } from "./project-nav.ts";
import { baselineOpened, type BenchMarks } from "./review-bench.ts";

/**
 * One browser-local owner of what the reader opened and marked viewed; the
 * bench, its stepper and the header comboboxes read it. Each account
 * baselines its own heads; local mode keeps the original keys.
 */
export function useBenchMarks(
  account: string | null,
  heads: ApiContext[],
  loaded: boolean,
  current: ApiContext | undefined,
) {
  const suffix = account === null ? "" : `:${account}`;
  const openedKey = `projects-seen${suffix}`;
  const viewedKey = `bench-viewed${suffix}`;
  const [opened, setOpened] = useState<SeenStreams | null>(() =>
    readSeen(saved(openedKey)),
  );
  const [viewed, setViewed] = useState<SeenStreams>(
    () => readSeen(saved(viewedKey)) ?? {},
  );
  useEffect(() => {
    if (opened === null && loaded)
      setOpened(baselineOpened(heads, Date.now(), contextActivity));
  }, [opened, loaded, heads]);
  const currentHead =
    current && heads.some((context) => context.id === current.id)
      ? current
      : undefined;
  useEffect(() => {
    if (!currentHead) return;
    const stream = streamKey(currentHead);
    setOpened((value) =>
      !value || value[stream] === currentHead.id
        ? value
        : { ...value, [stream]: currentHead.id },
    );
  }, [currentHead]);
  useEffect(() => {
    if (opened) save(openedKey, JSON.stringify(opened));
  }, [openedKey, opened]);
  useEffect(() => {
    save(viewedKey, JSON.stringify(viewed));
  }, [viewedKey, viewed]);
  /** Marks each head viewed until its stream publishes a newer one. */
  const markViewed = useCallback((contexts: ApiContext[]) => {
    if (!contexts.length) return;
    setViewed((value) => ({
      ...value,
      ...Object.fromEntries(
        contexts.map((context) => [streamKey(context), context.id]),
      ),
    }));
  }, []);
  /** Forgets that the stream was opened or viewed, so it reads as new. */
  const markUnread = useCallback((context: ApiContext) => {
    const stream = streamKey(context);
    const without = (value: SeenStreams) => {
      if (!(stream in value)) return value;
      const next = { ...value };
      delete next[stream];
      return next;
    };
    setOpened((value) => value && without(value));
    setViewed(without);
  }, []);
  const marks = useMemo<BenchMarks | null>(
    () => (opened ? { opened, viewed } : null),
    [opened, viewed],
  );
  return { marks, markViewed, markUnread };
}
