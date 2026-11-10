import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement, type ReactNode } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ApiContext, ApiPullRequest } from "@diffx/api";
import { AppearanceProvider } from "../../src/appearance-context.tsx";
import { ContextIdentity } from "../../src/ContextIdentity.tsx";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { ReviewBench } from "../../src/ReviewBench.tsx";
import { benchTrail } from "../../src/bench-trail.ts";
import { SessionProvider, useCatalog } from "../../src/session-context.tsx";
import { SidebarProvider } from "../../src/sidebar-context.tsx";
import { json, requests } from "./setup.ts";

Object.defineProperty(window, "matchMedia", {
  configurable: true,
  value: globalThis.matchMedia,
});
// Combobox lists walk the DOM with a TreeWalker.
Object.defineProperty(globalThis, "NodeFilter", {
  configurable: true,
  value: Reflect.get(window, "NodeFilter"),
});

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  localStorage.clear();
  sessionStorage.clear();
});

function head(
  id: string,
  repositoryId: string,
  lastChangedAt: number,
  pull?: Partial<ApiPullRequest>,
): ApiContext {
  const pullRequest: ApiPullRequest | undefined = pull && {
    provider: "github",
    repositoryId: 10,
    fullName: `octo/${repositoryId}`,
    number: 12,
    url: `https://github.com/octo/${repositoryId}/pull/12`,
    title: "Add search",
    body: "",
    state: "open",
    draft: false,
    author: { login: "mona", url: "https://github.com/mona" },
    baseRef: "main",
    headRef: "search",
    headRepository: "mona/app",
    createdAt: 1,
    updatedAt: 2,
    ...pull,
  };
  return {
    id,
    kind: "observation",
    source: "local",
    streamId: `stream-${id}`,
    latestContextId: id,
    name: repositoryId,
    branch: `branch-${id}`,
    worktreeName: null,
    root: `/repos/${repositoryId}`,
    repositoryId,
    locationId: null,
    createdAt: lastChangedAt,
    lastSubmittedAt: lastChangedAt,
    lastChangedAt,
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
        repositoryKey: repositoryId,
        repositoryName: repositoryId,
        remoteUrl: `https://github.com/octo/${repositoryId}.git`,
        checkoutKey: id,
        root: pullRequest.url,
        worktreeName: `PR #${pullRequest.number}`,
        branch: "search",
        head: "a".repeat(40),
        collectedAt: lastChangedAt,
        collectorVersion: "test",
        pullRequest,
      },
    }),
  };
}

// First visits count work idle for a day as opened; recent work is new.
const now = Date.now();
const day = 24 * 60 * 60 * 1000;
const local = head("local", "app", now);
const open = head("open", "app", now - 2 * day, {
  number: 7,
  title: "Open work",
  body: "Adds **search**.",
});
const merged = head("merged", "web", now, {
  number: 8,
  title: "Merged work",
  state: "merged",
});

function serve(contexts: ApiContext[], githubEnabled: boolean) {
  requests((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap")
      return json({
        mode: "remote",
        publicPullRequests: false,
        githubEnabled,
        googleEnabled: false,
        signupEnabled: false,
        passwordSignupEnabled: false,
        user: { id: "account-a", email: "alice@example.com", method: "google" },
        csrfToken: "csrf",
      });
    if (path === "/api/v2/contexts")
      return json({ contexts, nextCursor: null });
    if (path.endsWith("/diffs/current"))
      return json({
        files: [
          {
            id: "file-a",
            path: "src/search/index.ts",
            additions: 3,
            deletions: 1,
            fingerprint: "v1",
          },
          {
            id: "file-b",
            path: "README.md",
            additions: 2,
            deletions: 0,
            fingerprint: "v1",
          },
        ],
      });
    if (path.endsWith("/review-marks"))
      return json({
        marks: [
          {
            diffId: "diff",
            versionId: "version",
            fileId: "file-a",
            fileVersion: "v1",
            scope: "all",
          },
        ],
      });
    const found = contexts.find(
      (context) => path === `/api/v2/contexts/${context.id}`,
    );
    return found ? json(found) : json({ detail: "Not found" }, 404);
  });
}

function Identity() {
  const { current } = useCatalog();
  return current ? createElement(ContextIdentity, { current }) : null;
}

function Bench() {
  const { bench } = useCatalog();
  return bench ? createElement(ReviewBench) : null;
}

function mount(content: ReactNode = createElement(Bench)) {
  function Ready({ children }: { children: ReactNode }) {
    const { state } = useIdentity();
    return state.status === "ready"
      ? createElement(SessionProvider, null, children)
      : null;
  }
  return render(
    createElement(
      SidebarProvider,
      null,
      createElement(
        AppearanceProvider,
        null,
        createElement(
          IdentityProvider,
          null,
          createElement(Ready, null, content),
        ),
      ),
    ),
  );
}

function table() {
  return screen.findByRole("main", { name: "Reviews" });
}

function list(name: string) {
  return screen.findByRole("list", { name });
}

test("the bench leads with new work; viewed work waits under All open", async () => {
  serve([local, open, merged], true);
  mount();
  const main = await table();
  const unread = await list("New and updated");
  assert.ok(within(unread).getByRole("link", { name: /branch-local/ }));
  assert.equal(within(main).queryByText(/Open work/), null);
  assert.equal(within(main).queryByText(/Merged work/), null);

  const views = screen.getByRole("navigation", { name: "Review views" });
  fireEvent.click(within(views).getByRole("link", { name: /All open/ }));
  assert.equal(window.location.search, "?view=open");
  const rest = await list("Everything else");
  const row = within(rest).getByRole("link", { name: /#7 Open work/ });
  assert.ok(row.textContent?.includes("Viewed"));
  assert.ok(row.textContent?.includes("GitHub · @mona"));

  fireEvent.click(within(views).getByRole("link", { name: /Closed/ }));
  const closed = await list("Closed");
  assert.ok(within(closed).getByRole("link", { name: /#8 Merged work/ }));
  assert.equal(within(closed).queryByText(/branch-local/), null);
});

test("repository facets filter the list and clear on a second click", async () => {
  serve([local, open, merged], true);
  window.history.replaceState(null, "", "/?view=open");
  mount();
  const repositories = await screen.findByRole("list", {
    name: "Repositories",
  });
  const app = within(repositories).getByRole("link", { name: /app/ });
  assert.equal(app.getAttribute("href"), "/?view=open&repo=app");
  fireEvent.click(app);
  assert.equal(window.location.search, "?view=open&repo=app");
  await waitFor(() => assert.equal(app.getAttribute("aria-current"), "true"));
  fireEvent.click(app);
  assert.equal(window.location.search, "?view=open");
});

test("Mark all viewed empties Needs attention and persists per account", async () => {
  serve([local, open], true);
  mount();
  const section = await screen.findByRole("region", {
    name: "New and updated",
  });
  fireEvent.click(
    within(section).getByRole("button", { name: "Mark all viewed" }),
  );
  await screen.findByText("You're caught up");
  const viewed: unknown = JSON.parse(
    localStorage.getItem("bench-viewed:account-a") ?? "{}",
  );
  assert.deepEqual(viewed, { "stream-local": "local" });
  fireEvent.click(screen.getByRole("button", { name: "Show all open" }));
  assert.equal(window.location.search, "?view=open");
});

test("the peek shows a review's size, progress and where it changes", async () => {
  serve([local, open], true);
  window.history.replaceState(null, "", "/?view=open");
  mount();
  const peek = await screen.findByRole("complementary", { name: "Peek" });
  await within(peek).findByText("branch-local");
  await within(peek).findByText("1 of 2 files reviewed");
  assert.ok(within(peek).getByText("src/search"));
  assert.ok(within(peek).getByText("README.md"));

  const second = within(await list("Everything else")).getByRole("link", {
    name: /#7 Open work/,
  });
  fireEvent.focus(second);
  await within(peek).findByRole("heading", { name: "#7 Open work" });
  assert.ok(within(peek).getByText("search"));
  assert.ok(within(peek).getByRole("link", { name: /Open on GitHub/ }));
  fireEvent.click(within(peek).getByRole("button", { name: "Close peek" }));
  assert.equal(screen.queryByRole("complementary", { name: "Peek" }), null);
  assert.equal(localStorage.getItem("bench-peek"), "false");
});

test("with the peek a click previews a row; a double click opens it", async () => {
  serve([local, open], true);
  window.history.replaceState(null, "", "/?view=open");
  mount();
  const peek = await screen.findByRole("complementary", { name: "Peek" });
  const second = within(await list("Everything else")).getByRole("link", {
    name: /#7 Open work/,
  });
  fireEvent.click(second, { detail: 1 });
  await within(peek).findByRole("heading", { name: "#7 Open work" });
  assert.equal(second.getAttribute("aria-current"), "true");
  assert.equal(window.location.pathname, "/");
  fireEvent.click(second, { detail: 2 });
  await waitFor(() =>
    assert.equal(window.location.pathname, "/streams/stream-open"),
  );
});

test("without the peek a click opens the row", async () => {
  serve([local, open], true);
  localStorage.setItem("bench-peek", "false");
  window.history.replaceState(null, "", "/?view=open");
  mount();
  const second = within(await list("Everything else")).getByRole("link", {
    name: /#7 Open work/,
  });
  assert.equal(screen.queryByRole("complementary", { name: "Peek" }), null);
  fireEvent.click(second, { detail: 1 });
  await waitFor(() =>
    assert.equal(window.location.pathname, "/streams/stream-open"),
  );
});

test("the peek resizes from its left edge, persists and resets", async () => {
  const innerWidth = window.innerWidth;
  Object.defineProperty(window, "innerWidth", {
    configurable: true,
    value: 1600,
  });
  try {
    serve([local, open], true);
    localStorage.setItem("bench-peek-width", "9000");
    mount();
    const peek = await screen.findByRole("complementary", { name: "Peek" });
    const handle = within(peek).getByRole("separator", { name: "Resize peek" });
    assert.equal(handle.getAttribute("aria-valuenow"), "400");
    // The handle sits on the left edge: moving it left widens the peek.
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    assert.equal(handle.getAttribute("aria-valuenow"), "420");
    assert.equal(localStorage.getItem("bench-peek-width"), "420");
    assert.equal(
      document.documentElement.style.getPropertyValue("--bench-peek-width"),
      "420px",
    );
    for (let step = 0; step < 20; step++)
      fireEvent.keyDown(handle, { key: "ArrowLeft" });
    assert.equal(handle.getAttribute("aria-valuenow"), "640");
    for (let step = 0; step < 30; step++)
      fireEvent.keyDown(handle, { key: "ArrowRight" });
    assert.equal(handle.getAttribute("aria-valuenow"), "320");
    fireEvent.doubleClick(handle);
    assert.equal(handle.getAttribute("aria-valuenow"), "400");
  } finally {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: innerWidth,
    });
    document.documentElement.style.removeProperty("--bench-peek-width");
  }
});

test("arrow keys move the selection; opening a row keeps the bench's order", async () => {
  serve([local, open], true);
  window.history.replaceState(null, "", "/?view=open");
  mount();
  const first = within(await list("New and updated")).getByRole("link", {
    name: /branch-local/,
  });
  assert.equal(first.getAttribute("aria-current"), "true");
  assert.equal(first.getAttribute("tabindex"), "0");
  fireEvent.keyDown(first, { key: "ArrowDown" });
  const second = within(await list("Everything else")).getByRole("link", {
    name: /#7 Open work/,
  });
  await waitFor(() =>
    assert.equal(second.getAttribute("aria-current"), "true"),
  );
  assert.equal(first.getAttribute("tabindex"), "-1");
  assert.deepEqual(benchTrail(), {
    path: "/?view=open",
    order: ["stream-local", "stream-open"],
  });
  fireEvent.click(second);
  await waitFor(() =>
    assert.equal(window.location.pathname, "/streams/stream-open"),
  );
  assert.equal(window.location.search, "");
});

test("without pull requests the bench points to following repositories", async () => {
  serve([local], true);
  mount();
  await list("New and updated");
  assert.equal(
    screen
      .getByRole("link", { name: "Follow repositories" })
      .getAttribute("href"),
    "/settings/github",
  );
  cleanup();
  serve([local], false);
  mount();
  await list("New and updated");
  assert.equal(
    screen.queryByRole("link", { name: "Follow repositories" }),
    null,
  );
});

test("the branch switcher groups by section only when both list rows", async () => {
  serve([local, open, merged], true);
  window.history.replaceState(null, "", "/contexts/local");
  mount(createElement(Identity));
  const trigger = await screen.findByRole("combobox", {
    name: /Switch branch/,
  });
  fireEvent.click(trigger);
  const list = await screen.findByRole("listbox", { name: "app branches" });
  const groups = within(list).getAllByRole("group");
  assert.deepEqual(
    groups.map((group) => within(group).getAllByRole("option").length),
    [1, 1],
  );
  assert.ok(within(list).getByText("Pull requests"));
  assert.ok(within(list).getByText("From your machines"));
  assert.ok(
    within(groups[0] ?? list).getByRole("option", { name: /#7 Open work/ }),
  );

  cleanup();
  serve([local, head("other", "app", 80)], true);
  window.history.replaceState(null, "", "/contexts/local");
  mount(createElement(Identity));
  fireEvent.click(
    await screen.findByRole("combobox", { name: /Switch branch/ }),
  );
  const flat = await screen.findByRole("listbox", { name: "app branches" });
  assert.equal(within(flat).queryAllByRole("group").length, 0);
  assert.equal(within(flat).getAllByRole("option").length, 2);
});
