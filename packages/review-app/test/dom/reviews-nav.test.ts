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
import { ContextIdentity } from "../../src/ContextIdentity.tsx";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { ProjectsNav } from "../../src/ProjectsNav.tsx";
import { SessionProvider, useCatalog } from "../../src/session-context.tsx";
import { SidebarProvider } from "../../src/sidebar-context.tsx";
import { StreamNavProvider } from "../../src/stream-nav-context.tsx";
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

const local = head("local", "app", 100);
const open = head("open", "app", 50, { number: 7, title: "Open work" });
const merged = head("merged", "app", 900, {
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

function mount() {
  function Ready({ children }: { children: ReactNode }) {
    const { state } = useIdentity();
    return state.status === "ready"
      ? createElement(
          SidebarProvider,
          null,
          createElement(
            SessionProvider,
            null,
            createElement(StreamNavProvider, null, children),
          ),
        )
      : null;
  }
  return render(
    createElement(
      IdentityProvider,
      null,
      createElement(
        Ready,
        null,
        createElement(ProjectsNav),
        createElement(Identity),
      ),
    ),
  );
}

function reviews() {
  return screen.findByRole("navigation", { name: "Reviews" });
}

async function openViewMenu() {
  fireEvent.click(
    within(await reviews()).getByRole("button", { name: "Review list view" }),
  );
  return screen.findByRole("menu", { name: "Review list view" });
}

test("without GitHub the column keeps one list and no pull request filter", async () => {
  serve([local, open, merged], false);
  mount();
  const nav = await reviews();
  await within(nav).findByRole("link", { name: /branch-local/ });
  assert.ok(within(nav).getByRole("link", { name: /#7 Open work/ }));
  assert.equal(within(nav).queryByText(/Merged work/), null);
  assert.equal(screen.queryByRole("region", { name: "Pull requests" }), null);
  assert.equal(
    screen.queryByRole("region", { name: "From your machines" }),
    null,
  );
  const menu = await openViewMenu();
  assert.ok(within(menu).getByRole("menuitemradio", { name: "Group by repo" }));
  assert.equal(
    within(menu).queryByRole("group", { name: "Pull request state" }),
    null,
  );
  assert.equal(within(menu).queryByText("Pull requests"), null);
});

test("with GitHub, pull requests and machines get sections and the filter persists", async () => {
  serve([local, open, merged], true);
  mount();
  const pulls = await screen.findByRole("region", { name: "Pull requests" });
  const machines = screen.getByRole("region", { name: "From your machines" });
  // Pull requests lead; the active state reads beside the title.
  assert.ok(
    pulls.compareDocumentPosition(machines) & Node.DOCUMENT_POSITION_FOLLOWING,
  );
  assert.ok(
    within(pulls)
      .getByRole("button", { name: "Hide pull requests" })
      .textContent?.includes("Open"),
  );
  await within(pulls).findByRole("link", { name: /#7 Open work/ });
  assert.equal(within(pulls).queryByText(/Merged work/), null);
  assert.equal(within(pulls).queryByText(/branch-local/), null);
  assert.ok(within(machines).getByRole("link", { name: /branch-local/ }));

  const menu = await openViewMenu();
  const state = within(menu).getByRole("group", { name: "Pull request state" });
  assert.equal(
    within(state)
      .getByRole("menuitemradio", { name: "Open" })
      .getAttribute("aria-checked"),
    "true",
  );
  fireEvent.click(within(state).getByRole("menuitemradio", { name: "Merged" }));
  const row = await within(pulls).findByRole("link", {
    name: /#8 Merged work/,
  });
  assert.ok(row.textContent?.includes("GitHub · Merged · @mona"));
  assert.ok(within(row).getByRole("img", { name: "Merged pull request" }));
  assert.equal(within(pulls).queryByText(/Open work/), null);
  // Machines are never filtered.
  assert.ok(within(machines).getByRole("link", { name: /branch-local/ }));
  assert.equal(localStorage.getItem("streams-pr-filter"), "merged");

  cleanup();
  mount();
  const reloaded = await screen.findByRole("region", { name: "Pull requests" });
  await within(reloaded).findByRole("link", { name: /#8 Merged work/ });
  assert.ok(
    within(reloaded)
      .getByRole("button", { name: "Hide pull requests" })
      .textContent?.includes("Merged"),
  );
});

test("each filter explains an empty pull request section", async () => {
  const cases = [
    ["open", "No open pull requests. Follow repositories in Settings."],
    ["merged", "No merged pull requests."],
    ["all", "No pull requests."],
  ] as const;
  for (const [filter, copy] of cases) {
    localStorage.setItem("streams-pr-filter", filter);
    serve([local], true);
    mount();
    const pulls = await screen.findByRole("region", { name: "Pull requests" });
    await waitFor(() =>
      assert.equal(pulls.querySelector("p")?.textContent, copy),
    );
    if (filter === "open")
      assert.equal(
        within(pulls)
          .getByRole("link", { name: "Follow repositories" })
          .getAttribute("href"),
        "/settings/github",
      );
    cleanup();
    localStorage.clear();
  }
});

test("the root opens current work before a newer merged pull request", async () => {
  serve([merged, local], true);
  mount();
  await waitFor(() =>
    assert.equal(window.location.pathname, "/streams/stream-local"),
  );
});

test("the branch switcher groups by section only when both list rows", async () => {
  serve([local, open, merged], true);
  window.history.replaceState(null, "", "/contexts/local");
  mount();
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
  mount();
  fireEvent.click(
    await screen.findByRole("combobox", { name: /Switch branch/ }),
  );
  const flat = await screen.findByRole("listbox", { name: "app branches" });
  assert.equal(within(flat).queryAllByRole("group").length, 0);
  assert.equal(within(flat).getAllByRole("option").length, 2);
});
