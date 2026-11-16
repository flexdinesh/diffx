import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement } from "react";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ApiGitHubHistory, ApiGitHubHistoryOverview } from "@diffx/api";
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

/** An overview shaped by the request's edges: one count per bin. */
function overview(
  edges: number[],
  patch: Partial<ApiGitHubHistoryOverview> = {},
): ApiGitHubHistoryOverview {
  const bins = edges.slice(1).map((_, index) => ({
    since: edges[index] ?? 0,
    opened: index % 3,
    merged: index % 2,
  }));
  return {
    connected: true,
    login: "mona",
    following: true,
    retainedSince: now - 30 * day,
    since: edges[0] ?? 0,
    until: edges.at(-1) ?? 0,
    opened: 12,
    merged: 9,
    openNow: 4,
    medianToMerge: 5 * hour + 12 * 60_000,
    bins,
    repositories: [
      {
        fullName: "octo/app",
        opened: 8,
        merged: 9,
        openNow: 3,
        medianToMerge: 5 * hour + 12 * 60_000,
        merges: bins.map((bin) => bin.merged),
      },
      {
        fullName: "octo/api",
        opened: 4,
        merged: 0,
        openNow: 1,
        medianToMerge: null,
        merges: bins.map(() => 0),
      },
    ],
    stillOpen: [
      {
        repositoryId: 2,
        fullName: "octo/api",
        number: 3,
        title: "Draft the client",
        url: "https://github.com/octo/api/pull/3",
        draft: true,
        createdAt: now - 3 * day - 2 * hour,
      },
    ],
    truncated: false,
    ...patch,
  };
}

function list(patch: Partial<ApiGitHubHistory> = {}): ApiGitHubHistory {
  return {
    connected: true,
    login: "mona",
    following: true,
    retainedSince: now - 30 * day,
    since: now - 7 * day,
    until: now,
    opened: 1,
    merged: 1,
    repositories: [{ fullName: "octo/app", opened: 1, merged: 1 }],
    states: { open: 0, draft: 0, closed: 0, merged: 1 },
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
    ],
    nextCursor: null,
    ...patch,
  };
}

type Reads = {
  overview: number[][];
  list: URLSearchParams[];
};

function serve(
  answer: (edges: number[]) => ApiGitHubHistoryOverview | Response = (edges) =>
    overview(edges),
) {
  const reads: Reads = { overview: [], list: [] };
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
    if (url.pathname === "/api/v2/github/history/overview") {
      const edges = url.searchParams.getAll("edges").map(Number);
      reads.overview.push(edges);
      const result = answer(edges);
      return result instanceof Response ? result : json(result);
    }
    if (url.pathname === "/api/v2/github/history") {
      reads.list.push(url.searchParams);
      return json(list());
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

function localDay(offset: number) {
  const today = new Date();
  return new Date(
    today.getFullYear(),
    today.getMonth(),
    today.getDate() + offset,
  );
}

function dayParam(date: Date) {
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function chart() {
  return screen.getByRole("group", { name: /^Activity, / });
}

test("History opens on the overview of the last 7 days", async () => {
  const reads = serve();
  window.history.replaceState(null, "", "/history");
  mount();
  const strip = await screen.findByRole("list", { name: "Activity summary" });
  assert.deepEqual(
    within(strip)
      .getAllByRole("listitem")
      .map((cell) => cell.textContent),
    [
      "9 merged",
      "12 opened",
      "2 repos",
      "5h 12m median to merge",
      "4 still open",
    ],
  );
  // 7 local days chart as 7 daily bins.
  const edges = reads.overview[0] ?? [];
  assert.equal(edges.length, 8);
  assert.equal(edges[0], localDay(-6).getTime());
  assert.equal(edges.at(-1), localDay(1).getTime());
  assert.equal(reads.list.length, 0);
  assert.equal(window.location.search, "");
  const bins = within(chart()).getAllByRole("button");
  assert.equal(bins.length, 7);
  assert.match(
    bins[6]?.getAttribute("aria-label") ?? "",
    /^Today: 0 merged, 0 opened$/,
  );
  // The overview filters nothing, so the facets offer the range only.
  assert.ok(screen.getByRole("list", { name: "Range" }));
  assert.equal(screen.queryByRole("list", { name: "Activity" }), null);
  assert.equal(screen.queryByRole("button", { name: "Copy recap" }), null);
  const view = screen.getByRole("group", { name: "History view" });
  assert.equal(
    within(view)
      .getByRole("button", { name: "Overview" })
      .getAttribute("aria-pressed"),
    "true",
  );
});

test("arrow keys move between bins and a day bin opens its pull requests", async () => {
  const reads = serve();
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByRole("list", { name: "Activity summary" });
  const bins = within(chart()).getAllByRole("button");
  const [first, second] = bins;
  assert.ok(first && second);
  assert.equal(first.tabIndex, 0);
  act(() => first.focus());
  fireEvent.keyDown(first, { key: "ArrowRight" });
  assert.equal(document.activeElement, second);
  assert.equal(second.tabIndex, 0);
  fireEvent.click(second);
  const chosen = localDay(-5);
  assert.equal(window.location.search, `?view=list&day=${dayParam(chosen)}`);
  await screen.findByRole("link", { name: /#7 Add search/ });
  const read = reads.list[0];
  assert.equal(read?.get("since"), String(chosen.getTime()));
  assert.equal(read?.get("until"), String(localDay(-4).getTime()));
  // In the overview, the chosen day's bin shows selected.
  window.history.replaceState(null, "", `/history?day=${dayParam(chosen)}`);
  act(() => {
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await screen.findByRole("list", { name: "Activity summary" });
  const selected = within(chart())
    .getAllByRole("button")
    .filter((bin) => bin.getAttribute("aria-pressed") === "true");
  assert.equal(selected.length, 1);
  assert.equal(selected[0], within(chart()).getAllByRole("button")[1]);
});

test("repository rows link to their filtered pull requests", async () => {
  const reads = serve();
  window.history.replaceState(null, "", "/history?range=30d");
  mount();
  await screen.findByRole("list", { name: "Activity summary" });
  assert.equal(reads.overview[0]?.length, 31);
  const repositories = screen.getByRole("list", { name: "Repositories" });
  const row = within(repositories).getByRole("link", {
    name: "octo/app: 9 merged, 8 opened, 3 open now, 5h 12m median to merge",
  });
  assert.equal(
    row.getAttribute("href"),
    "/history?view=list&range=30d&repo=octo%2Fapp",
  );
  assert.ok(
    within(repositories).getByRole("link", {
      name: "octo/api: 0 merged, 4 opened, 1 open now",
    }),
  );
  fireEvent.click(row);
  assert.equal(window.location.search, "?view=list&range=30d&repo=octo%2Fapp");
  await waitFor(() => assert.equal(reads.list[0]?.get("repo"), "octo/app"));
});

test("still open pull requests link to GitHub, oldest first", async () => {
  serve();
  window.history.replaceState(null, "", "/history");
  mount();
  const stillOpen = await screen.findByRole("list", { name: "Still open" });
  const row = within(stillOpen).getByRole("link", {
    name: /#3 Draft the client/,
  });
  assert.equal(row.getAttribute("href"), "https://github.com/octo/api/pull/3");
  assert.equal(row.getAttribute("target"), "_blank");
  assert.ok(within(row).getByText("Draft"));
  assert.ok(within(row).getByText("open 3d 2h"));
  assert.ok(within(row).getByRole("img", { name: "Draft pull request" }));
  assert.ok(screen.getByText("Showing the oldest 1 of 4."));
});

test("bins before retained history are unavailable", async () => {
  serve((edges) => overview(edges, { retainedSince: now - 10 * day }));
  window.history.replaceState(null, "", "/history?range=30d");
  mount();
  await screen.findByText(/isn't kept/);
  const unavailable = within(chart())
    .getAllByRole("button")
    .filter((bin) =>
      / not available$/.test(bin.getAttribute("aria-label") ?? ""),
    );
  assert.ok(unavailable.length >= 19 && unavailable.length <= 21);
  assert.ok(
    unavailable.every((bin) => bin.getAttribute("aria-disabled") === "true"),
  );
});

test("ranges follow retention, and today charts hours with a now marker", async () => {
  serve((edges) => overview(edges, { retainedSince: now - 400 * day }));
  window.history.replaceState(null, "", "/history?range=today");
  mount();
  await screen.findByRole("list", { name: "Activity summary" });
  const ranges = screen.getByRole("list", { name: "Range" });
  assert.deepEqual(
    within(ranges)
      .getAllByRole("link")
      .map((link) => link.textContent),
    ["Today", "7 days", "30 days", "90 days", "1 year"],
  );
  const bins = within(chart()).getAllByRole("button");
  assert.ok(bins.length >= 23 && bins.length <= 25);
  // Hours after now are disabled.
  assert.ok(
    bins.some((bin) => bin.hasAttribute("disabled")) ||
      new Date().getHours() === 23,
  );
  assert.ok(screen.getByText("Now"));
  fireEvent.click(within(ranges).getByRole("link", { name: "1 year" }));
  assert.equal(window.location.search, "?range=1y");
  await waitFor(() =>
    assert.equal(within(chart()).getAllByRole("button").length, 52),
  );
});

test("a week bin opens that week's pull requests", async () => {
  serve((edges) => overview(edges, { retainedSince: now - 400 * day }));
  window.history.replaceState(null, "", "/history?range=1y");
  mount();
  await screen.findByRole("list", { name: "Activity summary" });
  const bins = within(chart()).getAllByRole("button");
  fireEvent.click(bins[50] ?? document.body);
  assert.equal(
    window.location.search,
    `?view=list&end=${dayParam(localDay(-7))}`,
  );
});

test("an empty window, no connection or a failure explain themselves", async () => {
  serve((edges) => overview(edges, { opened: 0, merged: 0, openNow: 0 }));
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("No pull requests in the last 7 days");
  assert.equal(screen.queryByRole("group", { name: /^Activity, / }), null);
  cleanup();
  serve((edges) => overview(edges, { connected: false, opened: 0, merged: 0 }));
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("Connect GitHub to see your history");
  cleanup();
  let fail = true;
  serve((edges) =>
    fail
      ? json(
          {
            title: "Bad Gateway",
            status: 502,
            detail: "GitHub request failed",
          },
          502,
        )
      : overview(edges),
  );
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByText("GitHub request failed");
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: /Retry/ }));
  await screen.findByRole("list", { name: "Activity summary" });
});

test("the view toggle pushes the list and Back restores the overview", async () => {
  const reads = serve();
  window.history.replaceState(null, "", "/history");
  mount();
  await screen.findByRole("list", { name: "Activity summary" });
  const view = screen.getByRole("group", { name: "History view" });
  fireEvent.click(within(view).getByRole("button", { name: "Pull requests" }));
  assert.equal(window.location.search, "?view=list");
  await screen.findByRole("link", { name: /#7 Add search/ });
  assert.equal(reads.list.length, 1);
  assert.ok(screen.getByRole("list", { name: "State now" }));
  window.history.replaceState(null, "", "/history");
  act(() => {
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  await screen.findByRole("list", { name: "Activity summary" });
  assert.equal(screen.queryByRole("list", { name: "State now" }), null);
});
