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
import type {
  ApiContext,
  ApiPublicPullRequest,
  ApiPullRequest,
} from "@diffx/api";
import { TooltipProvider } from "@diffx/ui/components/ui/tooltip";
import { AppearanceProvider } from "../../src/appearance-context.tsx";
import { SignIn, SignUp } from "../../src/account.tsx";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { Landing } from "../../src/Landing.tsx";
import {
  PublicLayout,
  PublicPullRequest,
} from "../../src/PublicPullRequest.tsx";
import { SidebarProvider } from "../../src/sidebar-context.tsx";
import { StreamNavProvider } from "../../src/stream-nav-context.tsx";
import { json, requests } from "./setup.ts";

// The appearance owner reads window.matchMedia; setup supplies the global.
Object.defineProperty(window, "matchMedia", {
  configurable: true,
  value: globalThis.matchMedia,
});

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  localStorage.clear();
  sessionStorage.clear();
});

const pull = { owner: "flexdinesh", repo: "diffx", number: 157 };
const statusPath = "/api/v2/public/pull-requests/flexdinesh/diffx/157";

const pullRequest: ApiPullRequest = {
  provider: "github",
  repositoryId: 1,
  fullName: "flexdinesh/diffx",
  number: 157,
  url: "https://github.com/flexdinesh/diffx/pull/157",
  title: "fix: allow cross-site page navigations",
  body: "",
  state: "merged",
  draft: false,
  author: { login: "flexdinesh", url: "https://github.com/flexdinesh" },
  baseRef: "main",
  headRef: "fix/navigation",
  headRepository: "flexdinesh/diffx",
  createdAt: 1,
  updatedAt: 2,
};

function status(
  value: Partial<ApiPublicPullRequest> = {},
): ApiPublicPullRequest {
  return {
    fullName: "flexdinesh/diffx",
    number: 157,
    status: "ready",
    pullRequest,
    contextId: "snapshot-a",
    fetchedAt: Date.now(),
    failure: null,
    ...value,
  };
}

function context(id: string): ApiContext {
  return {
    id,
    streamId: null,
    latestContextId: null,
    locationId: null,
    repositoryId: null,
    kind: "observation",
    createdAt: 1,
    lastSubmittedAt: 1,
    lastChangedAt: 1,
    changedFileCount: 1,
    expiresAt: null,
    submittedFrom: null,
    availability: "available",
    stale: false,
    root: "/flexdinesh/diffx",
    name: "diffx",
    branch: "fix/navigation",
    worktreeName: null,
    capabilities: {
      diff: {
        scopes: { state: "enabled", values: ["committed"] },
        refresh: { state: "unavailable" },
        stagingMetadata: { state: "unavailable" },
      },
      files: { contents: { state: "enabled" } },
      review: { comments: { state: "disabled" } },
    },
  };
}

function bootstrap(signedIn: boolean) {
  return {
    mode: "remote",
    publicPullRequests: true,
    googleEnabled: true,
    signupEnabled: true,
    passwordSignupEnabled: true,
    user: signedIn
      ? { id: "account-a", email: "alice@example.com", method: "password" }
      : null,
    csrfToken: signedIn ? "session-csrf" : null,
  };
}

/** Every request the page made, as "METHOD /path". */
function serve(
  handler: (request: Request, path: string) => Response | Promise<Response>,
  signedIn = false,
) {
  const seen: string[] = [];
  requests((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap") return json(bootstrap(signedIn));
    seen.push(`${request.method} ${path}`);
    return handler(request, path);
  });
  return seen;
}

function providers(content: ReturnType<typeof createElement>) {
  return createElement(
    SidebarProvider,
    null,
    createElement(
      StreamNavProvider,
      null,
      createElement(
        AppearanceProvider,
        null,
        createElement(
          IdentityProvider,
          null,
          createElement(TooltipProvider, null, content),
        ),
      ),
    ),
  );
}

/** The preview shell with a stand-in review: Pierre does not run in jsdom. */
function mountPreview() {
  function Content() {
    const { state } = useIdentity();
    if (state.status !== "ready") return null;
    return createElement(
      PublicPullRequest,
      { pull },
      createElement(
        PublicLayout,
        { ready: true, navigation: null },
        createElement("p", null, "Review ready"),
      ),
    );
  }
  return render(providers(createElement(Content)));
}

test("a visitor sees fetch progress, then the snapshot through public reads only", async () => {
  let polls = 0;
  const seen = serve((request, path) => {
    if (path === statusPath && request.method === "PUT")
      return json(
        status({ status: "fetching", contextId: null, fetchedAt: null }),
      );
    if (path === statusPath) {
      polls++;
      return json(status());
    }
    if (path === "/api/v2/public/contexts/snapshot-a")
      return json(context("snapshot-a"));
    return json({ detail: "Not found" }, 404);
  });
  mountPreview();
  // Metadata leads the snapshot, over the skeleton and fetch status.
  assert.ok(
    await screen.findByRole("heading", {
      name: "#157 fix: allow cross-site page navigations",
    }),
  );
  assert.ok(screen.getByText("Merged"));
  assert.ok(screen.getByText(/Fetching flexdinesh\/diffx from GitHub/));
  // The header offers plain sign-in; the preview notice returns to Settings.
  assert.deepEqual(
    screen
      .getAllByRole("link", { name: "Sign in" })
      .map((link) => link.getAttribute("href")),
    ["/signin", "/signin?next=/settings/github"],
  );
  assert.equal(
    screen
      .getByText(/^Read-only preview of a public pull request\. Sign in/)
      .closest("[role=status]")
      ?.querySelector("a")
      ?.getAttribute("href"),
    "/signin?next=/settings/github",
  );
  await screen.findByText("Review ready", undefined, { timeout: 5_000 });
  assert.equal(polls, 1);
  assert.ok(screen.getByText("Fetched just now"));
  assert.ok(screen.getByRole("button", { name: "Refresh pull request #157" }));
  assert.equal(
    screen.queryByRole("button", { name: "Account settings" }),
    null,
  );
  // The snapshot reads through the public routes; nothing account-scoped,
  // nothing comments, marks, catalog or events would need.
  assert.deepEqual(seen, [
    `PUT ${statusPath}`,
    `GET ${statusPath}`,
    "GET /api/v2/public/contexts/snapshot-a",
  ]);
});

test("refresh confirms an unchanged head, then offers a newer snapshot without swapping", async () => {
  let refreshed = 0;
  const seen = serve((request, path) => {
    if (path === statusPath && request.method === "PUT") return json(status());
    if (path === `${statusPath}/refresh`) {
      refreshed++;
      return json(
        refreshed === 1 ? status() : status({ contextId: "snapshot-b" }),
      );
    }
    if (path.startsWith("/api/v2/public/contexts/"))
      return json(context(path.split("/").at(-1) ?? ""));
    return json({ detail: "Not found" }, 404);
  });
  mountPreview();
  await screen.findByText("Review ready");
  const refresh = screen.getByRole("button", {
    name: "Refresh pull request #157",
  });
  fireEvent.click(refresh);
  await waitFor(() =>
    assert.ok(
      screen
        .getAllByRole("status")
        .some((region) => region.textContent === "Up to date"),
    ),
  );
  await waitFor(() => assert.equal(refresh.hasAttribute("disabled"), false));
  fireEvent.click(refresh);
  const latest = await screen.findByRole("button", { name: "View latest" });
  assert.ok(screen.getByText(/^Newer snapshot of flexdinesh\/diffx #157/));
  assert.equal(
    seen.filter((entry) => entry.endsWith("/contexts/snapshot-b")).length,
    0,
  );
  fireEvent.click(latest);
  await waitFor(() =>
    assert.ok(seen.includes("GET /api/v2/public/contexts/snapshot-b")),
  );
  await screen.findByText("Review ready");
  assert.equal(screen.queryByRole("button", { name: "View latest" }), null);
});

test("each failure replaces the skeleton with its explanation and recovery", async () => {
  const cases: {
    reply: () => Response;
    title: string;
    description: string;
    retry: boolean;
    github: boolean;
  }[] = [
    {
      reply: () =>
        json(
          status({
            status: "failed",
            contextId: null,
            fetchedAt: null,
            failure: { code: "not_found", detail: "private" },
          }),
        ),
      title: "Pull request unavailable",
      description: "This pull request isn't public or doesn't exist.",
      retry: false,
      github: true,
    },
    {
      reply: () => json({ detail: "Not found" }, 404),
      title: "Pull request unavailable",
      description: "This pull request isn't public or doesn't exist.",
      retry: false,
      github: true,
    },
    {
      reply: () =>
        json(
          status({
            status: "failed",
            contextId: null,
            fetchedAt: null,
            failure: { code: "too_large", detail: "size" },
          }),
        ),
      title: "Repository too large",
      description: "flexdinesh/diffx is over 500 MB, too large to preview.",
      retry: false,
      github: true,
    },
    {
      reply: () =>
        json(
          status({
            status: "failed",
            contextId: null,
            fetchedAt: null,
            failure: { code: "fetch_failed", detail: "clone" },
          }),
        ),
      title: "Cannot fetch pull request",
      description: "GitHub fetch failed.",
      retry: true,
      github: false,
    },
    {
      reply: () =>
        Response.json(
          { detail: "Too many requests" },
          { status: 429, headers: { "Retry-After": "150" } },
        ),
      title: "Preview limit reached",
      description: "Try again in 3 minutes.",
      retry: false,
      github: false,
    },
  ];
  for (const failure of cases) {
    const seen = serve((request, path) =>
      path === statusPath && request.method === "PUT"
        ? failure.reply()
        : json({ detail: "Unexpected" }, 500),
    );
    mountPreview();
    const heading = await screen.findByRole("heading", {
      level: 1,
      name: failure.title,
    });
    const state = heading.parentElement;
    assert.ok(state);
    assert.ok(within(state).getByText(failure.description));
    assert.equal(
      within(state).queryByRole("button", { name: "Retry" }) !== null,
      failure.retry,
      failure.title,
    );
    assert.equal(
      within(state)
        .queryByRole("link", { name: "Open on GitHub" })
        ?.getAttribute("href") ?? null,
      failure.github ? "https://github.com/flexdinesh/diffx/pull/157" : null,
    );
    if (failure.retry) {
      fireEvent.click(within(state).getByRole("button", { name: "Retry" }));
      await waitFor(() =>
        assert.equal(seen.filter((entry) => entry.startsWith("PUT")).length, 2),
      );
    }
    cleanup();
  }
});

/** The preview notice stays mounted while dismissed, hidden from everyone. */
function noticeShown() {
  const text = screen.queryByText(
    /^Read-only preview of a public pull request/,
  );
  return text !== null && text.closest("[hidden]") === null;
}

test("the preview notice stays dismissed in this browser", async () => {
  serve((request, path) =>
    path === statusPath
      ? json(status())
      : path === "/api/v2/public/contexts/snapshot-a"
        ? json(context("snapshot-a"))
        : json({ detail: "Not found" }, 404),
  );
  mountPreview();
  await screen.findByText("Review ready");
  assert.ok(screen.getByText(/^Read-only preview of a public pull request/));
  fireEvent.click(
    screen.getByRole("button", { name: "Dismiss preview notice" }),
  );
  assert.equal(noticeShown(), false);
  cleanup();
  mountPreview();
  await screen.findByText("Review ready");
  assert.equal(noticeShown(), false);
});

test("members get their account and Settings instead of Sign in", async () => {
  serve(
    (request, path) =>
      path === statusPath
        ? json(status())
        : path === "/api/v2/public/contexts/snapshot-a"
          ? json(context("snapshot-a"))
          : json({ detail: "Not found" }, 404),
    true,
  );
  mountPreview();
  await screen.findByText("Review ready");
  assert.ok(screen.getByRole("button", { name: "Account settings" }));
  assert.equal(screen.queryByRole("link", { name: "Sign in" }), null);
  assert.ok(
    screen.getByText(
      "Read-only preview of a public pull request. Follow your repositories in Settings to review their pull requests with your coding agent.",
    ),
  );
  assert.equal(
    screen.getByRole("link", { name: "Open Settings" }).getAttribute("href"),
    "/settings/github",
  );
});

test("the landing page validates a pasted link and opens its preview", () => {
  render(createElement(Landing));
  const field = screen.getByLabelText("Pull request URL");
  assert.equal(
    field.getAttribute("placeholder"),
    "https://github.com/owner/repo/pull/123",
  );
  fireEvent.change(field, {
    target: { value: "https://gitlab.com/flexdinesh/diffx/pull/157" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  const error = screen.getByRole("alert");
  assert.equal(
    error.textContent,
    "Enter a GitHub pull request link, like github.com/owner/repo/pull/123.",
  );
  assert.equal(field.getAttribute("aria-invalid"), "true");
  assert.equal(field.getAttribute("aria-describedby"), error.id);
  assert.equal(document.activeElement, field);
  assert.equal(window.location.pathname, "/");
  // Editing clears the error; a valid link opens the canonical path.
  fireEvent.change(field, { target: { value: "flexdinesh/diffx#157" } });
  assert.equal(screen.queryByRole("alert"), null);
  assert.equal(field.hasAttribute("aria-invalid"), false);
  fireEvent.click(screen.getByRole("button", { name: "Open" }));
  assert.equal(window.location.pathname, "/flexdinesh/diffx/pull/157");
  // Demos open the same way; sign-in moved to /signin.
  assert.equal(
    screen
      .getByRole("link", { name: /feat: redesign account settings layout/ })
      .getAttribute("href"),
    "/flexdinesh/diffx/pull/158",
  );
  assert.ok(screen.getByText(window.location.host));
  assert.equal(
    screen.getByRole("link", { name: "Sign in" }).getAttribute("href"),
    "/signin",
  );
});

function mountSignedOut(Page: typeof SignIn) {
  function Content() {
    const { state } = useIdentity();
    if (state.status !== "ready") return null;
    return state.bootstrap.user
      ? createElement("h1", null, "Signed in")
      : createElement(Page);
  }
  return render(providers(createElement(Content)));
}

test("sign-in returns to an allowed page only", async () => {
  const cases: [next: string, expected: string][] = [
    ["/settings/github", "/settings/github"],
    ["https://evil.example/settings/github", "/signin"],
  ];
  for (const [next, expected] of cases) {
    window.history.replaceState(null, "", `/signin?next=${next}`);
    serve((request, path) => {
      if (path === "/api/v2/auth/login")
        return json({ ...bootstrap(true), publicPullRequests: true });
      return json({ detail: "Unexpected" }, 500);
    });
    mountSignedOut(SignIn);
    fireEvent.change(await screen.findByLabelText("Email", { exact: true }), {
      target: { value: "alice@example.com" },
    });
    fireEvent.change(screen.getByLabelText("Password", { exact: true }), {
      target: { value: "a-secure-password" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
    await screen.findByRole("heading", { name: "Signed in" });
    assert.equal(window.location.pathname, expected, next);
    cleanup();
  }
});

test("Google sign-in and signup carry the allowed return page", async () => {
  const bodies: unknown[] = [];
  window.history.replaceState(null, "", "/signin?next=/settings/github");
  serve(async (request, path) => {
    assert.equal(path, "/api/v2/auth/google");
    bodies.push(await request.json());
    return json({ detail: "Google is unavailable" }, 503);
  });
  mountSignedOut(SignIn);
  fireEvent.click(
    await screen.findByRole("button", { name: "Continue with Google" }),
  );
  await screen.findByText("Google is unavailable");
  const create = screen.getByRole("link", { name: "Create an account" });
  assert.equal(
    create.getAttribute("href"),
    "/signup?next=%2Fsettings%2Fgithub",
  );
  cleanup();
  window.history.replaceState(null, "", "/signup?next=%2Fsettings%2Fgithub");
  mountSignedOut(SignUp);
  fireEvent.click(
    await screen.findByRole("button", { name: "Continue with Google" }),
  );
  await screen.findByText("Google is unavailable");
  assert.equal(
    screen.getByRole("link", { name: "Sign in" }).getAttribute("href"),
    "/signin?next=%2Fsettings%2Fgithub",
  );
  assert.deepEqual(bodies, [
    { next: "/settings/github" },
    { next: "/settings/github" },
  ]);
  cleanup();
  // Anything else is ignored, so Google returns home.
  window.history.replaceState(null, "", "/signin?next=/settings/tokens");
  mountSignedOut(SignIn);
  fireEvent.click(
    await screen.findByRole("button", { name: "Continue with Google" }),
  );
  await screen.findByText("Google is unavailable");
  assert.deepEqual(bodies.at(-1), {});
});
