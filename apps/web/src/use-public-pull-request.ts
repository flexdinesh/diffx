import { api, errorDetail, type ApiPublicPullRequest } from "@diffx/api";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  failureMessage,
  pullRequestName,
  retryAfterSeconds,
  retryLabel,
  type PullRequestRef,
} from "./public-pull-request.ts";

/** What the server said about the preview the page asked for. */
export type PreviewState =
  | { status: "loading" }
  /** Public previews are off, or the server cannot serve this one. */
  | { status: "unavailable" }
  /** The fetch limit was reached; another fetch is admitted at `retryAt`. */
  | { status: "limited"; retryAt: number }
  | { status: "error"; detail: string }
  | { status: "ok"; preview: ApiPublicPullRequest };

type Reply = {
  data?: ApiPublicPullRequest;
  error?: unknown;
  response: Response;
};

const loadFailure = "Unable to load the pull request. Try again.";
const pollMilliseconds = 2_000;
export const upToDate = "Up to date";

function settle(reply: Reply): PreviewState {
  if (reply.data) return { status: "ok", preview: reply.data };
  if (reply.response.status === 404) return { status: "unavailable" };
  if (reply.response.status === 429)
    return {
      status: "limited",
      retryAt:
        Date.now() +
        retryAfterSeconds(reply.response.headers.get("Retry-After")) * 1000,
    };
  return { status: "error", detail: errorDetail(reply.error, loadFailure) };
}

/** Queued and fetching previews are still being prepared. */
export function previewPending(preview: ApiPublicPullRequest | null) {
  return preview?.status === "queued" || preview?.status === "fetching";
}

/**
 * Owns one public pull request preview: ensures it on open (PUT), polls while
 * it is queued or fetching, refreshes on request, and keeps the snapshot the
 * reader is viewing. The first ready snapshot opens; later ones wait for
 * `viewLatest`, so a refresh never swaps the review in progress.
 */
export function usePublicPullRequest(pull: PullRequestRef) {
  const { owner, repo, number } = pull;
  const [state, setState] = useState<PreviewState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [poll, setPoll] = useState(0);
  const [waitingSince, setWaitingSince] = useState(Date.now);
  const [viewedId, setViewedId] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [feedback, setFeedback] = useState("");
  const preview = state.status === "ok" ? state.preview : null;
  const latestId = preview?.contextId ?? null;
  if (viewedId === null && latestId !== null) setViewedId(latestId);
  const viewed = useRef(viewedId);
  // A refresh reports its outcome once the preview settles.
  const awaitingRefresh = useRef(false);
  const lifetime = useRef(new AbortController());
  useEffect(() => {
    viewed.current = viewedId;
  }, [viewedId]);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => controller.abort();
  }, []);

  const name = pullRequestName(pull);
  const report = useCallback(
    (next: ApiPublicPullRequest) => {
      if (!awaitingRefresh.current || previewPending(next)) return;
      awaitingRefresh.current = false;
      if (next.status === "failed" && next.failure)
        setFeedback(failureMessage(next.failure.code, name));
      else if (next.contextId === viewed.current) setFeedback(upToDate);
    },
    [name],
  );

  useEffect(() => {
    const controller = new AbortController();
    void api
      .PUT("/api/v2/public/pull-requests/{owner}/{repo}/{number}", {
        params: { path: { owner, repo, number } },
        signal: controller.signal,
      })
      .then((reply) => {
        if (!controller.signal.aborted) setState(settle(reply));
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({
            status: "error",
            detail: errorDetail(error, loadFailure),
          });
      });
    return () => controller.abort();
  }, [owner, repo, number, attempt]);

  const pending = previewPending(preview);
  useEffect(() => {
    if (!pending) return;
    const controller = new AbortController();
    const timer = setTimeout(() => {
      void api
        .GET("/api/v2/public/pull-requests/{owner}/{repo}/{number}", {
          params: { path: { owner, repo, number } },
          signal: controller.signal,
        })
        .then((reply) => {
          if (controller.signal.aborted) return;
          if (reply.data) report(reply.data);
          // A failed check keeps an open snapshot and checks again.
          if (!reply.data && viewed.current) {
            setFeedback(errorDetail(reply.error, loadFailure));
            setPoll((value) => value + 1);
          } else setState(settle(reply));
        })
        .catch((error: unknown) => {
          if (controller.signal.aborted) return;
          if (viewed.current) {
            setFeedback(errorDetail(error, loadFailure));
            setPoll((value) => value + 1);
          } else
            setState({
              status: "error",
              detail: errorDetail(error, loadFailure),
            });
        });
    }, pollMilliseconds);
    return () => {
      clearTimeout(timer);
      controller.abort();
    };
  }, [pending, preview, poll, owner, repo, number, report]);

  // "Up to date" is a brief confirmation; other feedback stays until acted on.
  useEffect(() => {
    if (feedback !== upToDate) return;
    const timer = setTimeout(() => setFeedback(""), 4_000);
    return () => clearTimeout(timer);
  }, [feedback]);

  const refresh = useCallback(async () => {
    if (refreshing) return;
    const { signal } = lifetime.current;
    setRefreshing(true);
    setFeedback("");
    try {
      const reply = await api.POST(
        "/api/v2/public/pull-requests/{owner}/{repo}/{number}/refresh",
        { params: { path: { owner, repo, number } }, signal },
      );
      if (signal.aborted) return;
      if (reply.data) {
        awaitingRefresh.current = true;
        setWaitingSince(Date.now());
        setState({ status: "ok", preview: reply.data });
        report(reply.data);
      } else if (reply.response.status === 429)
        setFeedback(
          retryLabel(
            retryAfterSeconds(reply.response.headers.get("Retry-After")),
          ),
        );
      else
        setFeedback(
          errorDetail(reply.error, "Unable to refresh the pull request."),
        );
    } catch (error: unknown) {
      if (!signal.aborted)
        setFeedback(errorDetail(error, "Unable to refresh the pull request."));
    } finally {
      if (!signal.aborted) setRefreshing(false);
    }
  }, [refreshing, owner, repo, number, report]);

  const retry = useCallback(() => {
    setState({ status: "loading" });
    setFeedback("");
    setWaitingSince(Date.now());
    setAttempt((value) => value + 1);
  }, []);

  const newerId =
    latestId !== null && viewedId !== null && latestId !== viewedId
      ? latestId
      : null;
  const viewLatest = useCallback(() => {
    if (newerId) setViewedId(newerId);
  }, [newerId]);

  return {
    state,
    preview,
    pending,
    viewedId,
    newerId,
    viewLatest,
    refresh,
    refreshing,
    feedback,
    retry,
    waitingSince,
  };
}

export type PublicPreview = ReturnType<typeof usePublicPullRequest>;
