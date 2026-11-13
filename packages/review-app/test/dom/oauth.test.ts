import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement, type ComponentType } from "react";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import {
  IdentityProvider,
  IdentityStatus,
  useIdentity,
} from "../../src/identity-context.tsx";
import { AccountSettings } from "../../src/account.tsx";
import { OAuthConsent } from "../../src/oauth-consent.tsx";
import { json, requests } from "./setup.ts";

const requestId = "r".repeat(43);
const consentUrl = `/oauth/consent?request=${requestId}`;

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

const bootstrap = {
  mode: "remote",
  publicPullRequests: false,
  githubEnabled: false,
  googleEnabled: false,
  signupEnabled: false,
  passwordSignupEnabled: false,
  user: { id: "account-a", email: "alice@example.com", method: "google" },
  csrfToken: "session-csrf",
};

function mount(Page: ComponentType) {
  function Content() {
    const { state } = useIdentity();
    if (state.status !== "ready") return createElement(IdentityStatus);
    if (!state.bootstrap.user) return createElement("h1", null, "Sign in");
    return createElement(Page);
  }
  return render(createElement(IdentityProvider, null, createElement(Content)));
}

function server(handler: (request: Request) => Response | Promise<Response>) {
  requests((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/bootstrap") return json(bootstrap);
    if (path === "/api/v2/github/connection") return json({}, 404);
    if (path === "/api/v2/push") return json({}, 404);
    if (path === "/api/v2/account/tokens") return json({ tokens: [] });
    return handler(request);
  });
}

const verified = {
  client: { name: "Claude", verified: true, host: "claude.ai" },
  redirectHost: "127.0.0.1:53682",
  scopes: ["reviews:read", "comments:write"],
};

test("consent names a verified app, its redirect and lets comment writes be unticked", async () => {
  window.history.replaceState(null, "", consentUrl);
  // Returning to the client is a navigation; a fragment keeps jsdom on the page.
  const redirectTo = `http://localhost${consentUrl}#returned`;
  let body: unknown;
  server(async (request) => {
    assert.equal(
      new URL(request.url).pathname,
      `/api/v2/oauth/requests/${requestId}`,
    );
    if (request.method === "GET") return json(verified);
    assert.equal(request.headers.get("X-CSRF-Token"), "session-csrf");
    body = await request.json();
    return json({ redirectTo });
  });
  mount(OAuthConsent);
  await screen.findByRole("heading", {
    level: 1,
    name: "Connect Claude to diffx",
  });
  assert.ok(screen.getByText("Verified"));
  assert.ok(screen.getByText("claude.ai"));
  assert.ok(screen.getByText("127.0.0.1:53682"));
  assert.ok(screen.getByText("Read your reviews, diffs and comments"));
  assert.ok(screen.getByText("alice@example.com"));
  const comments = screen.getByRole("checkbox", {
    name: "Add comments, reply and resolve",
  });
  assert.equal(comments.getAttribute("aria-checked"), "true");
  fireEvent.click(comments);
  assert.equal(comments.getAttribute("aria-checked"), "false");
  fireEvent.click(screen.getByRole("button", { name: "Allow" }));
  await waitFor(() => assert.equal(window.location.hash, "#returned"));
  assert.deepEqual(body, { allow: true, scopes: ["reviews:read"] });
});

test("an unverified app says its name is its own claim; Cancel denies", async () => {
  window.history.replaceState(null, "", consentUrl);
  let body: unknown;
  server(async (request) => {
    if (request.method === "GET")
      return json({
        client: { name: "Cursor", verified: false, host: null },
        redirectHost: "localhost:4000",
        scopes: ["reviews:read"],
      });
    body = await request.json();
    return json({ redirectTo: `http://localhost${consentUrl}#denied` });
  });
  mount(OAuthConsent);
  await screen.findByRole("heading", { name: "Connect Cursor to diffx" });
  assert.ok(screen.getByText("Unverified app"));
  assert.ok(screen.getByText("Name provided by the app"));
  // Only read access was requested, so there is nothing to untick.
  assert.equal(screen.queryByRole("checkbox"), null);
  fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => assert.equal(window.location.hash, "#denied"));
  assert.deepEqual(body, { allow: false, scopes: ["reviews:read"] });
});

test("expired, answered or malformed requests show the error page", async () => {
  window.history.replaceState(null, "", consentUrl);
  server(() => json({ detail: "Gone" }, 404));
  mount(OAuthConsent);
  await screen.findByRole("heading", { name: "Can't connect this app" });
  cleanup();
  window.history.replaceState(null, "", "/oauth/consent?request=short");
  server(() => {
    throw new Error("A malformed request is never looked up");
  });
  mount(OAuthConsent);
  await screen.findByRole("heading", { name: "Can't connect this app" });
});

test("a consent response that is not an http URL is never followed", async () => {
  window.history.replaceState(null, "", consentUrl);
  server((request) =>
    request.method === "GET"
      ? json(verified)
      : json({ redirectTo: "javascript:alert(1)" }),
  );
  mount(OAuthConsent);
  fireEvent.click(await screen.findByRole("button", { name: "Allow" }));
  assert.match(
    (await screen.findByRole("alert")).textContent ?? "",
    /Invalid consent response/,
  );
  assert.equal(window.location.pathname, "/oauth/consent");
});

function agentsPage() {
  return createElement(AccountSettings, { section: "agents" });
}

test("connected agents list access in words and disconnect after confirmation", async () => {
  const calls: string[] = [];
  server((request) => {
    const path = new URL(request.url).pathname;
    calls.push(`${request.method} ${path}`);
    if (request.method === "DELETE") {
      assert.equal(request.headers.get("X-CSRF-Token"), "session-csrf");
      return new Response(null, { status: 204 });
    }
    return json({
      agents: [
        {
          id: "grant-1",
          name: "Claude",
          verified: true,
          host: "claude.ai",
          scopes: ["reviews:read", "comments:write"],
          createdAt: Date.UTC(2026, 0, 2),
          lastUsedAt: null,
        },
        {
          id: "grant-2",
          name: "Cursor",
          verified: false,
          host: null,
          scopes: ["reviews:read"],
          createdAt: Date.UTC(2026, 0, 3),
          lastUsedAt: Date.UTC(2026, 0, 4),
        },
      ],
    });
  });
  mount(agentsPage);
  const list = await screen.findByRole("list", { name: "Connected agents" });
  const rows = within(list).getAllByRole("listitem");
  assert.equal(rows.length, 2);
  assert.match(
    rows[0]?.textContent ?? "",
    /Claude.*Verified.*claude\.ai · Reads reviews and adds comments.*Never used/,
  );
  assert.match(
    rows[1]?.textContent ?? "",
    /Cursor.*Unverified.*Reads reviews.*Last used/,
  );
  fireEvent.click(screen.getByRole("button", { name: "Actions for Cursor" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "Disconnect…" }));
  fireEvent.click(await screen.findByRole("button", { name: "Disconnect" }));
  await screen.findByText("Disconnected Cursor.");
  assert.equal(calls.at(-1), "DELETE /api/v2/account/agents/grant-2");
  assert.equal(within(list).getAllByRole("listitem").length, 1);
});

test("without connected agents the section points to the MCP docs", async () => {
  server(() => json({ agents: [] }));
  mount(agentsPage);
  const link = await screen.findByRole("link", {
    name: "Connect one over MCP",
  });
  assert.equal(link.getAttribute("href"), "https://docs.diffx.app/mcp");
  assert.ok(screen.getByText(/No connected agents/));
});
