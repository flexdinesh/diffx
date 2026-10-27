import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import type { ApiChangedFile, ApiRepositoryDiff } from "@diffx/api";
import type { ReviewComment } from "../../src/review-comments.ts";
import { useReview } from "../../src/use-review.ts";
import { useReviewedFiles } from "../../src/use-reviewed-files.ts";
import { deferred, json, requests } from "./setup.ts";

afterEach(cleanup);

function file(path: string): ApiChangedFile {
  return {
    id: path,
    path,
    oldPath: null,
    status: "M",
    indexStatus: " ",
    worktreeStatus: "M",
    additions: 1,
    deletions: 1,
    binary: false,
    fingerprint: path,
  };
}
const first = file("first.ts");
const second = file("second.ts");
const repository: ApiRepositoryDiff = {
  id: "diff",
  versionId: "version",
  locationId: "location",
  repositoryId: "repo",
  source: "local",
  root: "/repo",
  name: "repo",
  branch: "main",
  head: "abc",
  mode: "all",
  revision: "version",
  files: [first, second],
};
const draft: ReviewComment = {
  id: "draft",
  diffId: "diff",
  versionId: "version",
  path: first.path,
  fingerprint: first.fingerprint,
  scope: "all",
  target: "file",
  side: "additions",
  start: 0,
  end: 0,
  code: "",
  body: "Submitted snapshot",
  status: "open",
  createdAt: 1,
};
const saved: ReviewComment = { ...draft, id: "saved-comment" };

function review(enabled = true) {
  return renderHook(() => {
    const [currentDraft, setDraft] = useState<ReviewComment | null>(draft);
    return {
      ...useReview("context", repository, currentDraft, setDraft, enabled),
      draft: currentDraft,
      setDraft,
    };
  });
}

test("duplicate submissions share one write; a late save updates newer text by saved ID", async () => {
  const pending = deferred<Response>();
  const writes: Request[] = [];
  let comments: ReviewComment[] = [];
  requests((request) => {
    if (request.method === "GET") return json({ comments });
    writes.push(request);
    return pending.promise;
  });
  const { result } = review();
  let saving: Promise<void> = Promise.resolve();
  act(() => {
    saving = result.current.submit();
    void result.current.submit();
  });
  await waitFor(() => assert.equal(writes.length, 1));
  assert.equal(result.current.pending.size, 1);
  act(() => result.current.setDraft({ ...draft, body: "Newer edit" }));
  comments = [saved];
  await act(async () => {
    pending.resolve(json(saved));
    await saving;
  });
  assert.equal(result.current.draft?.body, "Newer edit");
  assert.equal(result.current.draft?.id, saved.id);
  requests(async (request) => {
    if (request.method === "GET") return json({ comments });
    assert.equal(request.method, "PATCH");
    assert.equal(
      new URL(request.url).pathname,
      "/api/v2/contexts/context/comments/saved-comment",
    );
    assert.deepEqual(await request.json(), { body: "Newer edit" });
    comments = [{ ...saved, body: "Newer edit" }];
    return json(comments[0]);
  });
  await act(() => result.current.submit());
  assert.equal(result.current.draft, null);
});

test("late save cannot clear a replacement draft", async () => {
  const pending = deferred<Response>();
  let comments: ReviewComment[] = [];
  requests((request) =>
    request.method === "GET" ? json({ comments }) : pending.promise,
  );
  const { result } = review();
  let saving: Promise<void> = Promise.resolve();
  act(() => {
    saving = result.current.submit();
  });
  act(() => result.current.cancel());
  const replacement = { ...draft, id: "replacement", body: "Keep me" };
  act(() => result.current.setDraft(replacement));
  comments = [saved];
  await act(async () => {
    pending.resolve(json(saved));
    await saving;
  });
  assert.deepEqual(result.current.draft, replacement);
  assert.deepEqual(result.current.comments, [saved]);
});

test("failed save retains its draft and error, then retries", async () => {
  let fail = true;
  let comments: ReviewComment[] = [];
  requests((request) => {
    if (request.method === "GET") return json({ comments });
    if (fail) return json({ detail: "Save rejected" }, 500);
    comments = [saved];
    return json(saved);
  });
  const { result } = review();
  await act(() => result.current.submit());
  assert.equal(result.current.saveError?.message, "Save rejected");
  assert.equal(result.current.feedback, "Save rejected");
  assert.deepEqual(result.current.draft, draft);
  fail = false;
  await act(() => result.current.submit());
  assert.equal(result.current.draft, null);
  assert.equal(result.current.saveError, null);
});

test("slow polling completes without overlapping reads", async (t) => {
  t.mock.timers.enable({ apis: ["setInterval"] });
  const pending = deferred<Response>();
  let reads = 0;
  requests(() => {
    reads++;
    return reads === 1 ? json({ comments: [] }) : pending.promise;
  });
  const { result } = review();
  await act(async () => {});
  await act(async () => t.mock.timers.tick(3_000));
  assert.equal(reads, 2);
  await act(async () => t.mock.timers.tick(6_000));
  assert.equal(reads, 2);
  await act(async () => pending.resolve(json({ comments: [saved] })));
  assert.deepEqual(result.current.comments, [saved]);
});

for (const transport of ["toggle", "WebMCP"]) {
  test(`${transport} resolution ignores stale polling and preserves the draft`, async (t) => {
    t.mock.timers.enable({ apis: ["setInterval"] });
    const stale = deferred<Response>();
    let reads = 0;
    let fail = transport === "WebMCP";
    let comments = [saved];
    requests((request) => {
      if (request.method === "GET")
        return ++reads === 2 ? stale.promise : json({ comments });
      if (fail) return json({ detail: "Resolve rejected" }, 500);
      comments = [{ ...saved, status: "resolved" }];
      return json(
        transport === "toggle"
          ? comments[0]
          : { comment_id: saved.id, status: "resolved" },
      );
    });
    const { result } = review();
    await act(async () => {});
    assert.equal(result.current.comments.length, 1);
    if (fail) {
      await act(async () => {
        await assert.rejects(
          result.current.resolveComment(saved.id, new AbortController().signal),
          /Resolve rejected/,
        );
      });
      fail = false;
      // Failed writes refresh comments; leave that read pending as the stale poll.
    } else await act(async () => t.mock.timers.tick(3_000));
    await act(async () => {
      if (transport === "toggle") await result.current.toggle(saved);
      else
        await result.current.resolveComment(
          saved.id,
          new AbortController().signal,
        );
    });
    await act(async () => stale.resolve(json({ comments: [saved] })));
    assert.equal(result.current.comments[0]?.status, "resolved");
    assert.deepEqual(result.current.draft, draft);
  });
}

function reviewed() {
  return renderHook(() => {
    const [collapsed, setCollapsed] = useState(new Set<string>());
    return {
      ...useReviewedFiles("context", repository, "all", setCollapsed),
      collapsed,
    };
  });
}

for (const reverse of [false, true]) {
  test(`concurrent marks merge responses (${reverse ? "reverse" : "forward"} order)`, async () => {
    const a = deferred<Response>();
    const b = deferred<Response>();
    const marks: { fileId: string; fileVersion: string; scope: string }[] = [];
    requests((request) =>
      request.method === "GET"
        ? json({ marks })
        : request.url.includes(first.id)
          ? a.promise
          : b.promise,
    );
    const { result } = reviewed();
    await act(async () => {});
    let one: Promise<boolean> = Promise.resolve(false);
    let two: Promise<boolean> = Promise.resolve(false);
    act(() => {
      one = result.current.toggleReviewed(first);
      two = result.current.toggleReviewed(second);
    });
    for (const [file, response, completion] of reverse
      ? ([
          [second, b, two],
          [first, a, one],
        ] satisfies [ApiChangedFile, typeof a, Promise<boolean>][])
      : ([
          [first, a, one],
          [second, b, two],
        ] satisfies [ApiChangedFile, typeof a, Promise<boolean>][])) {
      marks.push({
        fileId: file.id,
        fileVersion: file.fingerprint,
        scope: "all",
      });
      await act(async () => {
        response.resolve(json({}));
        assert.equal(await completion, true);
      });
    }
    assert.equal(result.current.isReviewed(first), true);
    assert.equal(result.current.isReviewed(second), true);
    assert.deepEqual(
      result.current.collapsed,
      new Set([first.path, second.path]),
    );
  });
}

test("reset waits for pending marks and clears collapse state", async () => {
  const pending = deferred<Response>();
  let resets = 0;
  requests((request) => {
    if (request.method === "GET") return json({ marks: [] });
    if (request.method === "DELETE") {
      resets++;
      return json({});
    }
    return pending.promise;
  });
  const { result } = reviewed();
  let write: Promise<boolean> = Promise.resolve(false);
  let reset: Promise<void> = Promise.resolve();
  act(() => {
    write = result.current.toggleReviewed(first);
    reset = result.current.resetReviewed();
  });
  assert.equal(resets, 0);
  assert.equal(result.current.pending.has("*"), true);
  await act(async () => {
    pending.resolve(json({}));
    await write;
    await reset;
  });
  assert.equal(resets, 1);
  assert.equal(result.current.isReviewed(first), false);
  assert.equal(result.current.collapsed.size, 0);
});

test("reviewed-file read and write failures remain retryable independently of comments", async () => {
  let failRead = true;
  let failWrite = true;
  requests((request) =>
    request.method === "GET"
      ? failRead
        ? json({ detail: "Read rejected" }, 503)
        : json({ marks: [] })
      : failWrite
        ? json({ detail: "Mark rejected" }, 500)
        : json({}),
  );
  const { result } = reviewed();
  await waitFor(() => assert.equal(result.current.error, "Read rejected"));
  failRead = false;
  await act(async () => result.current.retry());
  assert.equal(result.current.error, "");
  await act(() => result.current.toggleReviewed(first));
  assert.equal(result.current.error, "Mark rejected");
  assert.equal(result.current.collapsed.size, 0);
  failWrite = false;
  await act(() => result.current.toggleReviewed(first));
  assert.equal(result.current.error, "");
  assert.equal(result.current.collapsed.has(first.path), true);
});

test("disabled comments issue no requests and cannot open a draft", async () => {
  let count = 0;
  requests(() => {
    count++;
    return json({ comments: [] });
  });
  const { result } = review(false);
  act(() => {
    result.current.cancel();
    result.current.begin(first);
  });
  await act(() => result.current.submit());
  assert.equal(count, 0);
  assert.equal(result.current.draft, null);
});
