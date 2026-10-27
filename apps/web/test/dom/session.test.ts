import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { createElement, type ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ApiContext } from "@diffx/api";
import { IdentityProvider, useIdentity } from "../../src/identity-context.tsx";
import { SidebarProvider } from "../../src/sidebar-context.tsx";
import {
  SessionProvider,
  useCatalog,
  useSessionShell,
} from "../../src/session-context.tsx";
import { deferred, json, requests } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
  localStorage.clear();
  sessionStorage.clear();
});

const context: ApiContext = {
  id: "local-review",
  streamId: null,
  latestContextId: null,
  locationId: "location",
  repositoryId: "repo",
  kind: "observation",
  createdAt: 1,
  lastSubmittedAt: 1,
  lastChangedAt: 30,
  changedFileCount: 1,
  expiresAt: null,
  submittedFrom: null,
  availability: "available",
  stale: false,
  root: "/local-review",
  name: "Local review",
  branch: "main",
  worktreeName: null,
  capabilities: {
    diff: {
      scopes: { state: "enabled", values: ["all", "staged", "unstaged"] },
      refresh: { state: "enabled" },
      stagingMetadata: { state: "enabled" },
    },
    files: { contents: { state: "enabled" } },
    review: { comments: { state: "enabled" } },
  },
};

function mount() {
  function Ready({ children }: { children: ReactNode }) {
    const { state } = useIdentity();
    return state.status === "ready"
      ? createElement(
          SidebarProvider,
          null,
          createElement(SessionProvider, null, children),
        )
      : null;
  }
  return renderHook(
    () => ({ catalog: useCatalog(), shell: useSessionShell() }),
    {
      wrapper: ({ children }) =>
        createElement(
          IdentityProvider,
          null,
          createElement(Ready, null, children),
        ),
    },
  );
}

function serve(
  handler: (request: Request) => Response | Promise<Response>,
  collect = false,
) {
  requests((request) =>
    new URL(request.url).pathname === "/api/v2/bootstrap"
      ? json({
          mode: "local",
          googleEnabled: false,
          user: null,
          csrfToken: null,
          collect,
          input: "checkouts",
        })
      : handler(request),
  );
}

test("landing selects the most active usable diff across catalog pages", async () => {
  const cursors: (string | null)[] = [];
  serve((request) => {
    const url = new URL(request.url);
    if (url.pathname !== "/api/v2/contexts") return json(context);
    const cursor = url.searchParams.get("cursor");
    cursors.push(cursor);
    return json(
      cursor
        ? {
            contexts: [
              {
                ...context,
                id: "later",
                lastChangedAt: 20,
                lastSubmittedAt: 900,
              },
              context,
            ],
            nextCursor: null,
          }
        : {
            contexts: [
              { ...context, id: "empty", changedFileCount: 0 },
              { ...context, id: "unknown", changedFileCount: null },
              { ...context, id: "unavailable", availability: "unavailable" },
            ],
            nextCursor: "next",
          },
    );
  });
  const { result } = mount();
  await waitFor(() => assert.equal(result.current?.shell.ready, true));
  assert.equal(result.current.catalog.selectedId, context.id);
  assert.equal(window.location.pathname, "/contexts/local-review");
  assert.deepEqual(cursors, [null, "next"]);
});

test("landing selects a diff when a catalog refresh adds changes", async () => {
  let changedFileCount = 0;
  serve((request) =>
    new URL(request.url).pathname === "/api/v2/contexts"
      ? json({ contexts: [{ ...context, changedFileCount }], nextCursor: null })
      : json(context),
  );
  const { result } = mount();
  await waitFor(() => assert.equal(result.current?.shell.empty, true));
  assert.equal(result.current.catalog.selectedId, "");
  changedFileCount = 1;
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await waitFor(() => assert.equal(result.current.shell.ready, true));
  assert.equal(result.current.catalog.selectedId, context.id);
});

test("session failure retries without reloading the page", async () => {
  let fail = true;
  serve((request) =>
    new URL(request.url).pathname === "/api/v2/contexts"
      ? json({ contexts: [context], nextCursor: null })
      : fail
        ? json({ detail: "Session unavailable" }, 503)
        : json(context),
  );
  const { result } = mount();
  await waitFor(() =>
    assert.equal(result.current?.shell.statusError, "Session unavailable"),
  );
  fail = false;
  act(() => result.current.shell.retry());
  await waitFor(() => assert.equal(result.current.shell.ready, true));
  assert.equal(result.current.catalog.selectedId, context.id);
});

test("live URLs resolve once; refreshed heads do not replace the selected snapshot", async () => {
  const old = { ...context, streamId: "main", latestContextId: context.id };
  const latest = { ...old, id: "new", latestContextId: "new" };
  let head: ApiContext | undefined = old;
  serve((request) =>
    new URL(request.url).pathname === "/api/v2/contexts"
      ? json({ contexts: head ? [head] : [], nextCursor: null })
      : json(request.url.endsWith("/new") ? latest : old),
  );
  window.history.replaceState(null, "", "/streams/main");
  const view = mount();
  await waitFor(() => assert.equal(view.result.current?.shell.ready, true));
  head = latest;
  act(() => document.dispatchEvent(new Event("visibilitychange")));
  await waitFor(() =>
    assert.equal(view.result.current.catalog.heads.contexts[0]?.id, "new"),
  );
  assert.equal(view.result.current.catalog.selectedId, old.id);
  view.unmount();
  const reload = mount();
  await waitFor(() => assert.equal(reload.result.current?.shell.ready, true));
  assert.equal(reload.result.current.catalog.selectedId, "new");
  reload.unmount();
  window.history.replaceState(null, "", "/contexts/local-review");
  const pinned = mount();
  await waitFor(() => assert.equal(pinned.result.current?.shell.ready, true));
  assert.equal(pinned.result.current.catalog.selectedId, old.id);
  pinned.unmount();
  head = undefined;
  window.history.replaceState(null, "", "/streams/main");
  const missing = mount();
  await waitFor(() =>
    assert.equal(missing.result.current?.shell.missing, true),
  );
  assert.equal(missing.result.current.shell.ready, false);
});

test("local root waits for collection's default; review column remains after fewer heads", async () => {
  const pending = deferred<Response>();
  const active = {
    ...context,
    id: "active",
    streamId: "active-stream",
    lastChangedAt: 900,
  };
  const launched = {
    ...context,
    id: "launched",
    streamId: "launched-stream",
    lastChangedAt: 100,
  };
  let heads = [active];
  let collections = 0;
  const opened: string[] = [];
  serve((request) => {
    const url = new URL(request.url);
    if (url.pathname === "/api/v2/collections") {
      collections++;
      return collections === 1
        ? pending.promise
        : json({
            contexts: heads,
            failures: [],
            defaultContextId: launched.id,
          });
    }
    if (url.pathname === "/api/v2/contexts")
      return json({ contexts: heads, nextCursor: null });
    opened.push(url.pathname);
    return json(launched);
  }, true);
  const { result } = mount();
  await waitFor(() => assert.equal(result.current?.shell.collecting, true));
  assert.equal(result.current.catalog.selectedId, "");
  assert.equal(result.current.catalog.reviewColumn, false);
  assert.deepEqual(opened, []);
  heads = [active, launched];
  await act(async () =>
    pending.resolve(
      json({ contexts: heads, failures: [], defaultContextId: launched.id }),
    ),
  );
  await waitFor(() => assert.equal(result.current.shell.ready, true));
  assert.equal(result.current.catalog.selectedId, launched.id);
  assert.equal(window.location.pathname, "/streams/launched-stream");
  assert.equal(result.current.catalog.reviewColumn, true);
  heads = [launched];
  await act(async () => result.current.catalog.collect?.());
  await waitFor(() =>
    assert.equal(result.current.catalog.heads.contexts.length, 1),
  );
  assert.equal(result.current.catalog.reviewColumn, true);
});

test("unresolved navigation joins collection without declaring a missing stream", async () => {
  const pending = deferred<Response>();
  let collections = 0;
  const snapshot = { ...context, streamId: "local-stream" };
  serve((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/collections") {
      collections++;
      return pending.promise;
    }
    return json(
      path === "/api/v2/contexts"
        ? { contexts: [], nextCursor: null }
        : snapshot,
    );
  }, true);
  const { result } = mount();
  await waitFor(() => assert.equal(collections, 1));
  act(() => {
    window.history.pushState(null, "", "/streams/local-stream");
    window.dispatchEvent(new PopStateEvent("popstate"));
  });
  assert.equal(result.current.shell.missing, false);
  assert.equal(result.current.shell.collecting, true);
  await act(async () =>
    pending.resolve(json({ contexts: [snapshot], failures: [] })),
  );
  await waitFor(() => assert.equal(result.current.shell.ready, true));
  assert.equal(collections, 1);
  assert.equal(result.current.catalog.selectedId, context.id);
});

for (const path of ["/", "/streams/local-stream"]) {
  test(`local startup failure retries from ${path} without stored reviews`, async () => {
    window.history.replaceState(null, "", path);
    const pending = deferred<Response>();
    let collections = 0;
    const snapshot = { ...context, streamId: "local-stream" };
    serve((request) => {
      const path = new URL(request.url).pathname;
      if (path === "/api/v2/collections")
        return ++collections === 1
          ? json({ detail: "git failed" }, 422)
          : pending.promise;
      return json(
        path === "/api/v2/contexts"
          ? { contexts: [], nextCursor: null }
          : snapshot,
      );
    }, true);
    const { result } = mount();
    await waitFor(() =>
      assert.equal(result.current?.shell.collectError, "git failed"),
    );
    assert.equal(result.current.shell.collecting, false);
    act(() => result.current.shell.retry());
    await waitFor(() => assert.equal(collections, 2));
    assert.equal(result.current.shell.collecting, true);
    await act(async () =>
      pending.resolve(
        json({
          contexts: [snapshot],
          failures: [],
          defaultContextId: context.id,
        }),
      ),
    );
    await waitFor(() => assert.equal(result.current.shell.ready, true));
    assert.equal(result.current.shell.collectError, "");
    assert.equal(window.location.pathname, "/streams/local-stream");
  });
}

test("local startup reports failed checkouts while opening collected changes", async () => {
  const snapshot = { ...context, streamId: "local-stream" };
  serve((request) => {
    const path = new URL(request.url).pathname;
    if (path === "/api/v2/collections")
      return json({
        contexts: [snapshot],
        failures: [{ path: "/broken", detail: "git failed" }],
        defaultContextId: context.id,
      });
    return json(
      path === "/api/v2/contexts"
        ? { contexts: [], nextCursor: null }
        : snapshot,
    );
  }, true);
  const { result } = mount();
  await waitFor(() => assert.equal(result.current?.shell.ready, true));
  assert.equal(
    result.current.shell.collectError,
    "Unable to sync 1 checkout: /broken (git failed)",
  );
  assert.equal(window.location.pathname, "/streams/local-stream");
});
