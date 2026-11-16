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
import type { ApiGitHubHistory, ApiGitHubHistoryPullRequest } from "@diffx/api";
import { HistoryPage } from "../../src/HistoryPage.tsx";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { SidebarProvider } from "../../src/sidebar-context.tsx";
import { json, requests } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  sessionStorage.clear();
  Reflect.deleteProperty(navigator, "clipboard");
});

const hour = 3_600_000;
const day = 24 * hour;
const now = Date.now();

const merged: ApiGitHubHistoryPullRequest = {
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
};

const draft: ApiGitHubHistoryPullRequest = {
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
};

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
    states: { open: 0, draft: 1, closed: 0, merged: 1 },
    pullRequests: [merged, draft],
    nextCursor: null,
    ...patch,
  };
}

type Read = {
  since: number;
  until: number;
  limit: string | null;
  cursor: string | null;
  repo: string | null;
  activity: string | null;
  state: string | null;
};

/** Answers history reads like the server: filters apply to the page only. */
function filtered(base: ApiGitHubHistory, read: Read): ApiGitHubHistory {
  return {
    ...base,
    pullRequests: base.pullRequests.filter(
      (pull) =>
        (!read.repo || pull.fullName === read.repo) &&
        (read.activity !== "merged" || pull.merged) &&
        (read.activity !== "opened" || pull.opened) &&
        (!read.state ||
          (read.state === "draft"
            ? pull.state === "open" && pull.draft
            : read.state === "open"
              ? pull.state === "open" && !pull.draft
              : pull.state === read.state)),
    ),
  };
}

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
      const params = url.searchParams;
      const read = {
        since: Number(params.get("since")),
        until: Number(params.get("until")),
        limit: params.get("limit"),
        cursor: params.get("cursor"),
        repo: params.get("repo"),
        activity: params.get("activity"),
        state: params.get("state"),
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

function localDay(offset: number) {
  const today = startOfToday();
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

test("the list sums up the week and lists pull requests by day", async () => {
  const reads = serve(() => history());
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByText(
    "You merged 1 and opened 2 pull requests across 2 repositories in the last 7 days.",
  );
  assert.deepEqual(reads[0], {
    since: localDay(-6).getTime(),
    until: localDay(1).getTime(),
    limit: "200",
    cursor: null,
    repo: null,
    activity: null,
    state: null,
  });
  const row = screen.getByRole("link", { name: /#7 Add search/ });
  assert.equal(row.getAttribute("href"), "https://github.com/octo/app/pull/7");
  assert.equal(row.getAttribute("target"), "_blank");
  assert.ok(row.textContent?.includes("Opened · Merged"));
  // Merged rows show how long the pull request took to merge.
  const duration = within(row).getByTitle("Time from open to merge");
  assert.equal(duration.textContent, "1h");
  assert.equal(
    within(
      screen.getByRole("link", { name: /#3 Draft the client/ }),
    ).queryByTitle("Time from open to merge"),
    null,
  );
  assert.ok(screen.getByRole("img", { name: "Draft pull request" }));
  assert.ok(screen.getByText(/kept 30 days/));
  const repositories = screen.getByRole("list", { name: "Repositories" });
  assert.ok(within(repositories).getByLabelText("1 merged, 1 opened"));
  // Facets count the whole window from the server's totals.
  const states = screen.getByRole("list", { name: "State now" });
  assert.ok(within(states).getByRole("link", { name: /Draft\s*1/ }));
  assert.ok(within(states).getByRole("link", { name: /Merged\s*1/ }));
});

test("facets filter on the server through the URL", async () => {
  const reads = serve((read) => filtered(history(), read));
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByRole("link", { name: /#3 Draft the client/ });
  const activity = screen.getByRole("list", { name: "Activity" });
  fireEvent.click(within(activity).getByRole("link", { name: /Merged/ }));
  assert.equal(window.location.search, "?view=list&activity=merged");
  await screen.findByText("You merged 1 pull request in the last 7 days.");
  assert.equal(reads.at(-1)?.activity, "merged");
  assert.equal(
    screen.queryByRole("link", { name: /#3 Draft the client/ }),
    null,
  );
  // Counts stay the window's totals while filtered.
  assert.ok(within(activity).getByRole("link", { name: /Opened\s*2/ }));
  fireEvent.click(within(activity).getByRole("link", { name: /Merged/ }));
  assert.equal(window.location.search, "?view=list");

  const states = screen.getByRole("list", { name: "State now" });
  fireEvent.click(within(states).getByRole("link", { name: /Draft/ }));
  assert.equal(window.location.search, "?view=list&state=draft");
  await waitFor(() => assert.equal(reads.at(-1)?.state, "draft"));
  await waitFor(() =>
    assert.equal(screen.queryByRole("link", { name: /#7 Add search/ }), null),
  );
  assert.ok(screen.getByRole("link", { name: /#3 Draft the client/ }));
  fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
  assert.equal(window.location.search, "?view=list");

  const repositories = screen.getByRole("list", { name: "Repositories" });
  fireEvent.click(
    within(repositories).getByRole("link", { name: /octo\/api/ }),
  );
  assert.equal(window.location.search, "?view=list&repo=octo%2Fapi");
  await waitFor(() => assert.equal(reads.at(-1)?.repo, "octo/api"));
  await waitFor(() =>
    assert.equal(screen.queryByRole("link", { name: /#7 Add search/ }), null),
  );
  fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
  assert.equal(window.location.search, "?view=list");
  await screen.findByRole("link", { name: /#7 Add search/ });
});

test("a day narrows the read to that day until the window steps", async () => {
  const reads = serve(() => history());
  const yesterday = localDay(-1);
  window.history.replaceState(
    null,
    "",
    `/history?view=list&day=${dayParam(yesterday)}`,
  );
  mount();
  await screen.findByText(/yesterday\.$/);
  assert.equal(reads[0]?.since, yesterday.getTime());
  assert.equal(reads[0]?.until, localDay(0).getTime());
  assert.equal(
    screen.getByText("Yesterday", { selector: ".history-window" }).textContent,
    "Yesterday",
  );
  fireEvent.click(screen.getByRole("button", { name: "Earlier" }));
  assert.match(window.location.search, /^\?view=list&end=\d{4}-\d{2}-\d{2}$/);
  await waitFor(() => assert.equal(reads.length, 2));
  assert.equal(reads[1]?.until, localDay(-6).getTime());
  // A day outside the window is ignored.
  cleanup();
  const outside = serve(() => history());
  window.history.replaceState(null, "", "/history?view=list&day=2001-01-01");
  mount();
  await screen.findByRole("link", { name: /#7 Add search/ });
  assert.equal(outside[0]?.since, localDay(-6).getTime());
  assert.equal(screen.queryByRole("button", { name: "Clear filters" }), null);
});

test("the stepper pages back through retained windows only", async () => {
  const reads = serve(() => history({ retainedSince: now - 10 * day }));
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByRole("link", { name: /#7 Add search/ });
  const later = screen.getByRole("button", { name: "Later" });
  assert.equal(later.hasAttribute("disabled"), true);
  fireEvent.click(screen.getByRole("button", { name: "Earlier" }));
  assert.match(window.location.search, /^\?view=list&end=\d{4}-\d{2}-\d{2}$/);
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
  assert.equal(window.location.search, "?view=list");
});

test("Load more appends the next page and groups across pages", async () => {
  const older: ApiGitHubHistoryPullRequest = {
    ...draft,
    repositoryId: 1,
    fullName: "octo/app",
    number: 5,
    title: "Older work",
    url: "https://github.com/octo/app/pull/5",
    draft: false,
    createdAt: now - 4 * day,
  };
  const reads = serve((read) =>
    read.cursor === "page-2"
      ? history({ pullRequests: [older], nextCursor: null })
      : history({ pullRequests: [merged, draft], nextCursor: "page-2" }),
  );
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByRole("link", { name: /#7 Add search/ });
  assert.ok(screen.getByText("2+ pull requests"));
  fireEvent.click(screen.getByRole("button", { name: "Load more" }));
  await screen.findByRole("link", { name: /#5 Older work/ });
  assert.equal(reads.at(-1)?.cursor, "page-2");
  assert.equal(reads.at(-1)?.limit, "200");
  assert.ok(screen.getByRole("link", { name: /#7 Add search/ }));
  assert.equal(screen.queryByRole("button", { name: "Load more" }), null);
  assert.ok(screen.getByText("3 pull requests"));
});

test("Copy recap reads every page and copies Markdown", async () => {
  const older: ApiGitHubHistoryPullRequest = {
    ...merged,
    number: 5,
    title: "Older *work*",
    url: "https://github.com/octo/app/pull/5",
    createdAt: now - 4 * day,
    mergedAt: now - 4 * day + 5 * hour + 12 * 60_000,
  };
  const reads = serve((read) =>
    read.cursor === "page-2"
      ? history({ pullRequests: [older], nextCursor: null })
      : history({ pullRequests: [merged, draft], nextCursor: "page-2" }),
  );
  let copied = "";
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: async (text: string) => {
        copied = text;
      },
    },
  });
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByRole("link", { name: /#7 Add search/ });
  fireEvent.click(screen.getByRole("button", { name: "Copy recap" }));
  await screen.findByRole("button", { name: "Copied" });
  assert.ok(screen.getByText("Recap copied"));
  assert.equal(reads.at(-1)?.cursor, "page-2");
  const lines = copied.split("\n");
  assert.match(lines[0] ?? "", /^\*\*Pull requests · .+\*\*$/);
  assert.equal(
    lines[2],
    "You merged 1 and opened 2 pull requests across 2 repositories in the last 7 days.",
  );
  assert.ok(
    copied.includes(
      "\n**octo/app**\n- Merged [#7 Add search](https://github.com/octo/app/pull/7) · 1h to merge\n",
    ),
  );
  assert.ok(
    copied.includes(
      "- Merged [#5 Older \\*work\\*](https://github.com/octo/app/pull/5) · 5h 12m to merge",
    ),
  );
  assert.ok(
    copied.includes(
      "**octo/api**\n- Opened [#3 Draft the client](https://github.com/octo/api/pull/3), draft, still open",
    ),
  );
  assert.ok(!copied.includes("latest 1,000"));
});

test("a refused clipboard shows the recap for manual copying", async () => {
  serve(() => history());
  Object.defineProperty(navigator, "clipboard", {
    configurable: true,
    value: {
      writeText: async () => {
        throw new Error("Denied");
      },
    },
  });
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByRole("link", { name: /#7 Add search/ });
  fireEvent.click(screen.getByRole("button", { name: "Copy recap" }));
  const dialog = await screen.findByRole("dialog", { name: "Copy recap" });
  const text = within(dialog).getByLabelText("Recap");
  assert.ok(text instanceof HTMLTextAreaElement);
  assert.match(text.value, /^\*\*Pull requests · /);
  fireEvent.click(within(dialog).getByRole("button", { name: "Close" }));
  await screen.findByRole("button", { name: "Copy recap" });
});

test("History explains how to start without GitHub or follows", async () => {
  const empty = { pullRequests: [], repositories: [], opened: 0, merged: 0 };
  serve(() => history({ connected: false, following: false, ...empty }));
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByText("Connect GitHub to see your history");
  assert.equal(
    screen.getByRole("link", { name: "Connect GitHub" }).getAttribute("href"),
    "/settings/github",
  );
  cleanup();
  serve(() => history({ following: false, ...empty }));
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByText("Follow repositories to see your history");
  cleanup();
  serve(() => history(empty));
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByText("No pull requests in the last 7 days");
  assert.equal(
    screen.getByRole("button", { name: "Copy recap" }).hasAttribute("disabled"),
    true,
  );
  fireEvent.click(screen.getByRole("button", { name: "Show 30 days" }));
  assert.equal(window.location.search, "?view=list&range=30d");
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
  window.history.replaceState(null, "", "/history?view=list");
  mount();
  await screen.findByText("GitHub request failed");
  fail = false;
  fireEvent.click(screen.getByRole("button", { name: /Retry/ }));
  await screen.findByRole("link", { name: /#7 Add search/ });
});
