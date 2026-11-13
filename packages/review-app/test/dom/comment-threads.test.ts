import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { useState } from "react";
import type { ApiRepositoryDiff } from "@diffx/api";
import type { CommentReply, ReviewComment } from "../../src/review-comments.ts";
import { useReview } from "../../src/use-review.ts";
import { eventStreams, json, requests } from "./setup.ts";

afterEach(cleanup);

function hint(name: string, data: unknown) {
  const [stream] = eventStreams;
  assert.ok(stream, "no event stream");
  act(() => {
    stream.dispatchEvent(
      new window.MessageEvent(name, { data: JSON.stringify(data) }),
    );
  });
}

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
  files: [],
};
const comment: ReviewComment = {
  id: "comment",
  diffId: "diff",
  versionId: "version",
  path: "file.ts",
  fingerprint: "file.ts",
  scope: "all",
  target: "file",
  side: "additions",
  start: 0,
  end: 0,
  code: "",
  body: "Check this",
  status: "open",
  createdAt: 1,
  author: { kind: "user" },
};

function review() {
  return renderHook(() => {
    const [draft, setDraft] = useState<ReviewComment | null>(null);
    return {
      ...useReview(
        "context",
        "stream",
        "http://localhost/streams/stream",
        repository,
        draft,
        setDraft,
        true,
      ),
      draft,
      setDraft,
    };
  });
}

test("replies post to the thread and close their composer only once saved", async () => {
  let comments = [comment];
  const posted: unknown[] = [];
  requests(async (request) => {
    if (request.method === "GET") return json({ comments });
    assert.equal(
      new URL(request.url).pathname,
      "/api/v2/contexts/context/comments/comment/replies",
    );
    posted.push(await request.json());
    if (posted.length === 1) return json({ detail: "Busy" }, 503);
    comments = [
      {
        ...comment,
        replies: [
          {
            id: "reply",
            author: { kind: "user" },
            body: "Why?",
            createdAt: 2,
          },
        ],
      },
    ];
    return json(comments[0], 201);
  });
  const { result } = review();
  await waitFor(() => assert.equal(result.current.loaded, true));
  act(() => result.current.setReplyDraft("comment", " Why? "));
  let saved = true;
  await act(async () => {
    saved = await result.current.addReply(comment, " Why? ");
  });
  assert.equal(saved, false);
  assert.equal(result.current.replyDrafts.get("comment"), " Why? ");
  await act(async () => {
    saved = await result.current.addReply(comment, " Why? ");
  });
  assert.equal(saved, true);
  assert.deepEqual(posted, [{ body: "Why?" }, { body: "Why?" }]);
  assert.equal(result.current.replyDrafts.has("comment"), false);
  assert.equal(result.current.comments[0]?.replies?.[0]?.body, "Why?");
});

test("comment events from this stream reload saved comments and keep drafts and composers", async () => {
  let comments = [comment];
  let reads = 0;
  requests((request) => {
    assert.equal(request.method, "GET");
    reads++;
    return json({ comments });
  });
  const { result } = review();
  await waitFor(() => assert.equal(result.current.loaded, true));
  act(() => {
    result.current.setDraft({ ...comment, id: "draft", body: "Unsaved" });
    result.current.setReplyDraft("comment", "Half a reply");
  });
  const before = reads;
  hint("comments", { contextId: "other", streamId: "elsewhere" });
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal(reads, before, "another stream's change reloaded comments");

  // An agent replies through another snapshot of the same stream.
  comments = [
    {
      ...comment,
      replies: [
        {
          id: "agent",
          author: { kind: "agent", name: "Claude" },
          body: "Fixed",
          createdAt: 3,
        },
      ],
    },
  ];
  hint("comments", { contextId: "newer", streamId: "stream" });
  await waitFor(() =>
    assert.equal(result.current.comments[0]?.replies?.[0]?.body, "Fixed"),
  );
  assert.equal(result.current.draft?.body, "Unsaved");
  assert.equal(result.current.replyDrafts.get("comment"), "Half a reply");

  // An empty ingestion hint means changes may have been missed.
  comments = [{ ...comment, status: "resolved", resolvedBy: { kind: "user" } }];
  hint("ingestion", { contextId: "" });
  await waitFor(() =>
    assert.equal(result.current.comments[0]?.status, "resolved"),
  );
});

test("thread edits and deletes address the reply and copy links name the comment", async () => {
  const reply: CommentReply = {
    id: "reply",
    author: { kind: "user" },
    body: "First",
    createdAt: 2,
  };
  let comments: ReviewComment[] = [{ ...comment, replies: [reply] }];
  const writes: string[] = [];
  requests(async (request) => {
    if (request.method === "GET") return json({ comments });
    writes.push(`${request.method} ${new URL(request.url).pathname}`);
    if (request.method === "PATCH") {
      assert.deepEqual(await request.json(), { body: "Revised" });
      comments = [
        { ...comment, replies: [{ ...reply, body: "Revised", editedAt: 4 }] },
      ];
      return json(comments[0]);
    }
    comments = [{ ...comment, replies: [] }];
    return new Response(null, { status: 204 });
  });
  let copied = "";
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: async (text: string) => {
        copied = text;
      },
    },
  });
  const { result } = review();
  await waitFor(() => assert.equal(result.current.loaded, true));
  await act(async () => {
    assert.equal(
      await result.current.editReply(comment, reply, " Revised "),
      true,
    );
  });
  assert.equal(result.current.comments[0]?.replies?.[0]?.editedAt, 4);
  await act(() => result.current.deleteReply(comment, reply));
  assert.deepEqual(result.current.comments[0]?.replies, []);
  assert.deepEqual(writes, [
    "PATCH /api/v2/contexts/context/comments/comment/replies/reply",
    "DELETE /api/v2/contexts/context/comments/comment/replies/reply",
  ]);
  act(() => result.current.copyLink(comment));
  await waitFor(() =>
    assert.equal(copied, "http://localhost/streams/stream?comment=comment"),
  );
});
