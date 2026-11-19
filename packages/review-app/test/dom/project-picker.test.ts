import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement } from "react";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ApiContext, ApiPullRequest } from "@diffx/api";
import {
  ProjectPicker,
  type PickerPullRequestLinks,
} from "../../src/ProjectPicker.tsx";
import type { PullRequestRef } from "../../src/public-pull-request.ts";

// jsdom does not lay out, so the picker's scroll to the active option is a no-op.
Object.defineProperty(Element.prototype, "scrollIntoView", {
  configurable: true,
  value: () => {},
});

afterEach(cleanup);

function head(id: string, pullRequest?: ApiPullRequest): ApiContext {
  return {
    id,
    kind: "observation",
    streamId: `stream-${id}`,
    latestContextId: id,
    name: "diffx",
    branch: `branch-${id}`,
    worktreeName: null,
    root: `/repos/${id}`,
    repositoryId: `repo-${id}`,
    locationId: null,
    createdAt: 1,
    lastSubmittedAt: 1,
    lastChangedAt: 1,
    changedFileCount: 2,
    expiresAt: null,
    submittedFrom: null,
    availability: "available",
    stale: false,
    capabilities: {
      diff: {
        scopes: { state: "enabled", values: ["all"] },
        refresh: { state: "unavailable" },
        stagingMetadata: { state: "enabled" },
      },
      files: { contents: { state: "enabled" } },
      review: { comments: { state: "enabled" } },
    },
    ...(pullRequest && {
      observation: {
        sourceId: "github",
        hostname: "github.com",
        runId: "",
        agent: "",
        trigger: "github",
        repositoryKey: "diffx",
        repositoryName: "diffx",
        remoteUrl: "https://github.com/flexdinesh/diffx.git",
        checkoutKey: id,
        root: pullRequest.url,
        worktreeName: `PR #${pullRequest.number}`,
        branch: pullRequest.headRef,
        head: "a".repeat(40),
        collectedAt: 1,
        collectorVersion: "test",
        pullRequest,
      },
    }),
  };
}

const followed = head("pr-224", {
  provider: "github",
  repositoryId: 1,
  fullName: "flexdinesh/diffx",
  number: 224,
  url: "https://github.com/flexdinesh/diffx/pull/224",
  title: "docs: README for the diffx server",
  body: "",
  state: "open",
  draft: false,
  author: { login: "flexdinesh", url: "https://github.com/flexdinesh" },
  baseRef: "main",
  headRef: "docs/readme",
  headRepository: "flexdinesh/diffx",
  createdAt: 1,
  updatedAt: 2,
});

function mount(links: boolean) {
  const opened: [PullRequestRef, ApiContext | undefined][] = [];
  const pullRequestLinks: PickerPullRequestLinks | null = links
    ? { heads: [followed], open: (pull, found) => opened.push([pull, found]) }
    : null;
  render(
    createElement(ProjectPicker, {
      contexts: [head("main"), followed],
      selectedId: "",
      select: () => {},
      open: true,
      onOpenChange: () => {},
      pullRequestLinks,
    }),
  );
  return opened;
}

const field = () => screen.getByRole("combobox", { name: "Search reviews" });

function type(value: string) {
  fireEvent.change(field(), { target: { value } });
}

test("the picker says it opens pull request links and offers it as an option", () => {
  mount(true);
  assert.equal(
    field().getAttribute("placeholder"),
    "Search repositories, or paste a pull request link…",
  );
  assert.ok(screen.getByText("Paste a pull request link", { exact: false }));
  fireEvent.click(screen.getByRole("option", { name: /Open a pull request/ }));
  assert.equal(
    field().getAttribute("placeholder"),
    "https://github.com/owner/repo/pull/123",
  );
  assert.ok(screen.getByText(/Paste a link like github\.com/));
  assert.equal(screen.queryByRole("option", { name: /diffx/ }), null);
  fireEvent.click(screen.getByRole("button", { name: "Back to reviews" }));
  assert.equal(
    field().getAttribute("placeholder"),
    "Search repositories, or paste a pull request link…",
  );
});

test("a pasted link previews a public pull request or opens the bench review", () => {
  const opened = mount(true);
  type("https://github.com/vercel/next.js/pull/71234/files");
  const preview = screen.getByRole("option", { name: /vercel\/next\.js/ });
  assert.equal(preview.getAttribute("aria-selected"), "true");
  assert.ok(preview.textContent?.includes("Preview"));
  fireEvent.keyDown(field(), { key: "Enter" });
  assert.deepEqual(opened, [
    [{ owner: "vercel", repo: "next.js", number: 71234 }, undefined],
  ]);

  type("flexdinesh/diffx#224");
  const tracked = screen.getByRole("option", { name: /flexdinesh\/diffx/ });
  assert.ok(tracked.textContent?.includes("On your bench"));
  assert.ok(tracked.textContent?.includes("docs: README for the diffx server"));
  fireEvent.click(tracked);
  assert.equal(opened.at(-1)?.[1]?.id, "pr-224");
});

test("other hosts' links name the problem instead of searching", () => {
  mount(true);
  type("https://gitlab.com/acme/api/-/merge_requests/9");
  assert.ok(screen.getByText("Only github.com pull request links open here."));
  assert.equal(screen.queryAllByRole("option").length, 0);
});

test("without pull request links the picker only searches reviews", () => {
  mount(false);
  assert.equal(field().getAttribute("placeholder"), "Search repositories…");
  assert.equal(
    screen.queryByRole("option", { name: /Open a pull request/ }),
    null,
  );
  type("github.com/vercel/next.js/pull/1");
  assert.equal(screen.queryByText("Pull request link"), null);
});
