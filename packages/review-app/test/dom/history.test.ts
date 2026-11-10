import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ApiGitHubHistory } from "@diffx/api";
import { HistoryPage } from "../../src/HistoryPage.tsx";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { SidebarProvider } from "../../src/sidebar-context.tsx";
import { json, requests } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  sessionStorage.clear();
});

const hour = 3_600_000;
const day = 24 * hour;
const now = Date.now();

function history(patch: Partial<ApiGitHubHistory> = {}): ApiGitHubHistory {
  return {
    connected: true,
    login: "mona",
    following: true,
    retainedSince: now - 30 * day,
    since: now - 7 * day,
    until: now,
    opened: 2,
    merged: 1,
    repositories: [
      { fullName: "octo/app", opened: 1, merged: 1 },
      { fullName: "octo/api", opened: 1, merged: 0 },
    ],
    pullRequests: [
      {
        repositoryId: 1,
        fullName: "octo/app",
        number: 7,
        title: "Add search",
        url: "https://github.com/octo/app/pull/7",
        draft: false,
        state: "merged",
        createdAt: now - 2 * hour,
        mergedAt: now - hour,
        closedAt: now - hour,
        opened: true,
        merged: true,
      },
      {
        repositoryId: 2,
        fullName: "octo/api",
        number: 3,
        title: "Draft the client",
        url: "https://github.com/octo/api/pull/3",
        draft: true,
        state: "open",
        createdAt: now - 3 * day,
        mergedAt: null,
        closedAt: null,
        opened: true,
        merged: false,
      },
    ],
    truncated: false,
    ...patch,
  };
}

type Read = { since: number; until: number };

function serve(answer: (read: Read) => ApiGitHubHistory | Response) {
  const reads: Read[] = [];
  requests((request) => {
    const url = new URL(request.url);
    if (url.pathname === "/api/v2/bootstrap")
      return json({
        mode: "remote",
        publicPullRequests: false,
        githubEnabled: true,
        googleEnabled: false,
        signupEnabled: false,
        passwordSignupEnabled: false,
        user: { id: "account-a", email: "alice@example.com", method: "google" },
        csrfToken: "csrf",
      });
    if (url.pathname === "/api/v2/github/history") {
      const read = {
        since: Number(url.searchParams.get("since")),
        until: Number(url.searchParams.get("until")),
      };
      reads.push(read);
      const result = answer(read);
      return result instanceof Response ? result : json(result);
    }
    throw new Error(`Unexpected request: ${url.pathname}`);
  });
  return reads;
}

function mount() {
  function Ready() {
    const { state } = useIdentity();
    return state.status === "ready" ? createElement(HistoryPage) : null;
  }
  return render(
    createElement(
      SidebarProvider,
      null,
      createElement(IdentityProvider, null, createElement(Ready)),
    ),
  );
}

function startOfToday() {
  const today = new Date();
  return new Date(today.getFullYear(), today.getMonth(), today.getDate());
}

test("History sums up the week and lists pull requests by day", async () => {
  const reads = serve(() => history());
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText(
    "You merged 1 and opened 2 pull requests across 2 repositories in the last 7 days.",
  );
  const today = startOfToday();
  assert.deepEqual(reads[0], {
    since: new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate() - 6,
    ).getTime(),
    until: new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate() + 1,
    ).getTime(),
  });
  const row = screen.getByRole("link", { name: /#7 Add search/ });
  assert.equal(row.getAttribute("href"), "https://github.com/octo/app/pull/7");
  assert.equal(row.getAttribute("target"), "_blank");
  assert.ok(row.textContent?.includes("Opened · Merged"));
  assert.ok(screen.getByRole("img", { name: "Draft pull request" }));
  assert.ok(screen.getByText(/kept 30 days/));
  const repositories = screen.getByRole("list", { name: "Repositories" });
  assert.ok(within(repositories).getByLabelText("1 merged, 1 opened"));
});

test("facets filter by activity and repository through the URL", async () => {
  serve(() => history());
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByRole("link", { name: /#3 Draft the client/ });
  const activity = screen.getByRole("list", { name: "Activity" });
  fireEvent.click(within(activity).getByRole("link", { name: /Merged/ }));
  assert.equal(window.location.search, "?activity=merged");
  assert.equal(
    screen.queryByRole("link", { name: /#3 Draft the client/ }),
    null,
  );
  await screen.findByText("You merged 1 pull request in the last 7 days.");
  fireEvent.click(within(activity).getByRole("link", { name: /Merged/ }));
  assert.equal(window.location.search, "");

  const repositories = screen.getByRole("list", { name: "Repositories" });
  fireEvent.click(
    within(repositories).getByRole("link", { name: /octo\/api/ }),
  );
  assert.equal(window.location.search, "?repo=octo%2Fapi");
  assert.equal(screen.queryByRole("link", { name: /#7 Add search/ }), null);
  fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
  assert.equal(window.location.search, "");
  assert.ok(screen.getByRole("link", { name: /#7 Add search/ }));
});

test("the stepper pages back through retained windows only", async () => {
  const reads = serve(() => history({ retainedSince: now - 10 * day }));
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByRole("link", { name: /#7 Add search/ });
  const later = screen.getByRole("button", { name: "Later" });
  assert.equal(later.hasAttribute("disabled"), true);
  fireEvent.click(screen.getByRole("button", { name: "Earlier" }));
  assert.match(window.location.search, /^\?end=\d{4}-\d{2}-\d{2}$/);
  await waitFor(() => assert.equal(reads.length, 2));
  const [first, second] = reads;
  assert.equal(second?.until, first?.since);
  await screen.findByText(/isn't kept/);
  await waitFor(() =>
    assert.equal(
      screen.getByRole("button", { name: "Earlier" }).hasAttribute("disabled"),
      true,
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Later" }));
  assert.equal(window.location.search, "");
});

test("History explains how to start without GitHub or follows", async () => {
  serve(() =>
    history({
      connected: false,
      following: false,
      pullRequests: [],
      repositories: [],
      opened: 0,
      merged: 0,
    }),
  );
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("Connect GitHub to see your history");
  assert.equal(
    screen.getByRole("link", { name: "Connect GitHub" }).getAttribute("href"),
    "/settings/github",
  );
  cleanup();
  serve(() =>
    history({
      following: false,
      pullRequests: [],
      repositories: [],
      opened: 0,
      merged: 0,
    }),
  );
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("Follow repositories to see your history");
  cleanup();
  serve(() =>
    history({ pullRequests: [], repositories: [], opened: 0, merged: 0 }),
  );
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("No pull requests in the last 7 days");
  fireEvent.click(screen.getByRole("button", { name: "Show 30 days" }));
  assert.equal(window.location.search, "?range=30d");
});

test("a failed read explains why and retries", async () => {
  let fail = true;
  serve(() =>
    fail
      ? json(
          {
            title: "Bad Gateway",
            status: 502,
            detail: "GitHub request failed",
          },
          502,
        )
      : history(),
  );
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("GitHub request failed");
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: /Retry/ }));
  await screen.findByRole("link", { name: /#7 Add search/ });
});
