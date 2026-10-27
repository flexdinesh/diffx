import {
  type CodeViewItem,
  parseDiffFromFile,
  parsePatchFiles,
  setLanguageOverride,
} from "@pierre/diffs";
import {
  api,
  errorDetail,
  type ApiChangedFile as ChangedFile,
  type ApiFilePatch as FilePatch,
  type ApiRepositoryDiff as RepositoryDiff,
} from "@diffx/api";
import type { DiffMode } from "./diff-mode.ts";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CommentAnnotation } from "./review-model.ts";
import { languageOverride } from "./display-options.ts";
import { filesInTreeOrder } from "./file-tree.ts";

// Each load or "Load more" places previews in file order until the next one
// would pass this many bytes, bounding browser memory per step.
const previewWindowBytes = 16 * 1024 * 1024;
const notLoadedMessage =
  "Not loaded: 16 MiB of previews loaded. Use Load more to continue.";

/** A review scope's current diff, or one captured commit's read-only diff. */
export type DiffTarget =
  | { kind: "scope"; mode: DiffMode }
  | { kind: "commit"; oid: string };

async function getDiff(
  contextId: string,
  target: DiffTarget,
  signal: AbortSignal,
) {
  const { data, error } =
    target.kind === "commit"
      ? await api.GET("/api/v2/contexts/{contextId}/commits/{oid}/diff", {
          params: { path: { contextId, oid: target.oid } },
          signal,
        })
      : await api.GET("/api/v2/contexts/{contextId}/diffs/current", {
          params: { path: { contextId }, query: { scope: target.mode } },
          signal,
        });
  if (!data) throw new Error(errorDetail(error, "Unable to load changes"));
  return data;
}

async function getPatch(
  contextId: string,
  target: DiffTarget,
  data: RepositoryDiff,
  file: ChangedFile,
  signal: AbortSignal,
) {
  return target.kind === "commit"
    ? api.GET(
        "/api/v2/contexts/{contextId}/commits/{oid}/files/{fileId}/patch",
        {
          params: {
            path: { contextId, oid: target.oid, fileId: file.id },
            query: { fileVersion: file.fingerprint },
          },
          signal,
        },
      )
    : api.GET(
        "/api/v2/contexts/{contextId}/diffs/{diffId}/files/{fileId}/patch",
        {
          params: {
            path: { contextId, diffId: data.id, fileId: file.id },
            query: {
              scope: target.mode,
              versionId: data.versionId,
              fileVersion: file.fingerprint,
            },
          },
          signal,
        },
      );
}
function messageItem(
  file: ChangedFile,
  message: string,
): CodeViewItem<CommentAnnotation> {
  return {
    id: file.path,
    type: "file",
    file: { name: file.path, contents: `${message}\n`, lang: "text" },
  };
}
interface Preview {
  fingerprint: string;
  item: CodeViewItem<CommentAnnotation>;
  additions: number;
  deletions: number;
}
type Fetched =
  | { ok: true; bytes: number; preview: Preview }
  | { ok: false; item: CodeViewItem<CommentAnnotation> };
interface DiffState {
  repository: RepositoryDiff | null;
  items: Map<string, CodeViewItem<CommentAnnotation>>;
  busy: boolean;
  notice: string;
  connected: boolean;
  /** Files after the last loaded preview, waiting for Load more. */
  unloaded: number;
}
const initialState: DiffState = {
  repository: null,
  items: new Map(),
  busy: false,
  notice: "",
  connected: false,
  unloaded: 0,
};

function buildPreview(
  file: ChangedFile,
  body: FilePatch,
  cacheKey: string,
): Preview {
  const contents = body.contents ?? null;
  const parsed =
    contents && body.message === null
      ? parseDiffFromFile(
          { name: file.path, contents: contents.before },
          { name: file.path, contents: contents.after },
        )
      : body.patch
        ? parsePatchFiles(body.patch, file.fingerprint, true).flatMap(
            (patch) => patch.files,
          )[0]
        : undefined;
  if (parsed && parsed.hunks.length > 0) {
    const firstLine =
      parsed.hunks[0]?.additionStart === 1
        ? parsed.additionLines[0]
        : parsed.hunks[0]?.deletionStart === 1
          ? parsed.deletionLines[0]
          : undefined;
    const language = languageOverride(file.path, firstLine);
    const displayDiff = language
      ? setLanguageOverride(parsed, language)
      : parsed;
    // Reuse worker highlighting across navigation and scope reloads.
    displayDiff.cacheKey = cacheKey;
    displayDiff.name = file.path;
    if (file.oldPath) displayDiff.prevName = file.oldPath;
    return {
      fingerprint: file.fingerprint,
      item: { id: file.path, type: "diff", fileDiff: displayDiff },
      additions: displayDiff.hunks.reduce(
        (sum, hunk) => sum + hunk.additionLines,
        0,
      ),
      deletions: displayDiff.hunks.reduce(
        (sum, hunk) => sum + hunk.deletionLines,
        0,
      ),
    };
  }
  return {
    fingerprint: file.fingerprint,
    item: messageItem(
      file,
      body.message ??
        (body.patch.includes("Binary files")
          ? "Binary file changed. No text preview available."
          : file.oldPath
            ? `Renamed from ${file.oldPath}. No text changes.`
            : "No text changes (empty file or file metadata changed)."),
    ),
    additions: file.additions,
    deletions: file.deletions,
  };
}

// Observations are immutable. Scope changes abort stale requests; retries reload
// the same stored snapshot and preserve successfully loaded previews.
export function useDiff(
  contextId: string,
  requested: DiffTarget,
  enabled = true,
) {
  const [state, setState] = useState<DiffState>(initialState);
  const targetKey =
    requested.kind === "commit" ? `commit:${requested.oid}` : requested.mode;
  const key = `${contextId}:${targetKey}:${enabled}`;
  // Effects follow the target's identity, not the caller's object.
  const [target, setTarget] = useState(requested);
  if (
    (target.kind === "commit" ? `commit:${target.oid}` : target.mode) !==
    targetKey
  )
    setTarget(requested);
  const [loadedKey, setLoadedKey] = useState("");
  const cache = useRef(new Map<string, Preview>());
  const refreshRef = useRef<() => void>(() => {});
  const loadMoreRef = useRef<() => void>(() => {});
  const refresh = useCallback(() => refreshRef.current(), []);
  const loadMore = useCallback(() => loadMoreRef.current(), []);
  useEffect(() => {
    let disposed = false;
    let controller = new AbortController();
    cache.current.clear();
    setState(initialState);
    setLoadedKey(key);
    loadMoreRef.current = () => {};
    if (!enabled) {
      refreshRef.current = () => {};
      return;
    }
    async function load() {
      if (disposed) return;
      controller.abort();
      controller = new AbortController();
      const signal = controller.signal;
      loadMoreRef.current = () => {};
      setState((previous) => ({ ...previous, busy: true }));
      try {
        const data: RepositoryDiff = await getDiff(contextId, target, signal);
        if (signal.aborted || disposed) return;
        const paths = new Set(data.files.map((file) => file.path));
        for (const path of cache.current.keys())
          if (!paths.has(path)) cache.current.delete(path);
        const items = new Map<string, CodeViewItem<CommentAnnotation>>();
        // Pierre requires one item object per rendered contents; files that
        // return to "Loading" or "Not loaded" reuse their earlier item.
        const messages = new Map<string, CodeViewItem<CommentAnnotation>>();
        function status(file: ChangedFile, message: string) {
          const key = `${file.path}\n${message}`;
          const item = messages.get(key) ?? messageItem(file, message);
          messages.set(key, item);
          items.set(file.path, item);
        }
        const pending: ChangedFile[] = [];
        // Load in the viewer's file order.
        for (const file of filesInTreeOrder(data.files)) {
          const cached = cache.current.get(file.path);
          if (cached?.fingerprint === file.fingerprint) {
            items.set(file.path, cached.item);
            file.additions = cached.additions;
            file.deletions = cached.deletions;
          } else {
            status(file, "Loading diff…");
            pending.push(file);
          }
        }
        // Fetches run ahead concurrently and render as they arrive. A window
        // keeps the leading queued files whose previews fit its budget; a
        // later preview shown early is withdrawn when an earlier one fills
        // the window, and stays fetched for the next window.
        let queue = pending;
        const fetched = new Map<string, Fetched>();
        let next = 0;
        let cut = -1;
        let loading = false;
        let failed = 0;
        let windowFailed = 0;
        function publish(busy: boolean) {
          const failures = failed + windowFailed;
          setState({
            repository: {
              ...data,
              files: data.files.map((file) => ({ ...file })),
            },
            items: new Map(items),
            busy,
            notice: failures
              ? `${failures} file previews could not load. Refresh to retry.`
              : "",
            connected: true,
            unloaded: loading ? 0 : queue.length,
          });
        }
        function layout() {
          let bytes = 0;
          cut = -1;
          windowFailed = 0;
          for (const [index, file] of queue.entries()) {
            const result = fetched.get(file.path);
            if (!result) status(file, "Loading diff…");
            else if (!result.ok) {
              windowFailed++;
              items.set(file.path, result.item);
            } else if (bytes > 0 && bytes + result.bytes > previewWindowBytes) {
              cut = index;
              break;
            } else {
              bytes += result.bytes;
              file.additions = result.preview.additions;
              file.deletions = result.preview.deletions;
              items.set(file.path, result.preview.item);
            }
          }
          if (cut >= 0)
            for (const file of queue.slice(cut)) status(file, notLoadedMessage);
        }
        async function fetchPreview(file: ChangedFile): Promise<Fetched> {
          try {
            const { data: body, error } = await getPatch(
              contextId,
              target,
              data,
              file,
              signal,
            );
            if (!body)
              throw new Error(errorDetail(error, "Unable to load diff"));
            const bytes =
              body.patch.length +
              (body.contents?.before.length ?? 0) +
              (body.contents?.after.length ?? 0);
            const cacheKey = JSON.stringify([
              contextId,
              data.id,
              targetKey,
              file.path,
              file.fingerprint,
            ]);
            return {
              ok: true,
              bytes,
              preview: buildPreview(file, body, cacheKey),
            };
          } catch (error) {
            return {
              ok: false,
              item: messageItem(
                file,
                error instanceof Error ? error.message : "Unable to load diff",
              ),
            };
          }
        }
        async function fetchNext() {
          while (cut < 0 && next < queue.length && !signal.aborted) {
            const file = queue[next++];
            if (!file || fetched.has(file.path)) continue;
            const result = await fetchPreview(file);
            if (signal.aborted || disposed) return;
            fetched.set(file.path, result);
            layout();
            publish(true);
          }
        }
        async function loadWindow() {
          loading = true;
          next = 0;
          layout();
          publish(true);
          await Promise.all(Array.from({ length: 4 }, fetchNext));
          loading = false;
          if (signal.aborted || disposed) return;
          // Claims stop at the cut, so every file before it has a result.
          const end = cut < 0 ? queue.length : cut;
          for (const file of queue.slice(0, end)) {
            const result = fetched.get(file.path);
            if (result?.ok) cache.current.set(file.path, result.preview);
            fetched.delete(file.path);
          }
          failed += windowFailed;
          windowFailed = 0;
          queue = queue.slice(end);
          publish(false);
        }
        loadMoreRef.current = () => {
          if (!loading && queue.length > 0 && !signal.aborted && !disposed)
            void loadWindow();
        };
        await loadWindow();
      } catch (error) {
        if (signal.aborted || disposed) return;
        setState((previous) => ({
          ...previous,
          connected: false,
          notice:
            error instanceof Error
              ? error.message
              : "Unable to connect to the server",
        }));
      } finally {
        if (!signal.aborted && !disposed) {
          setState((previous) => ({ ...previous, busy: false }));
        }
      }
    }
    refreshRef.current = () => void load();
    void load();
    return () => {
      disposed = true;
      controller.abort();
    };
  }, [contextId, target, targetKey, enabled, key]);
  // Never expose the previous scope during the render before effect cleanup.
  const current = loadedKey === key ? state : initialState;
  return useMemo(
    () => ({ ...current, refresh, loadMore }),
    [current, refresh, loadMore],
  );
}
