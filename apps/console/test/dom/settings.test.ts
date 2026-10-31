import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { createElement } from "react";
import { Settings } from "../../src/settings.tsx";
import { csrfToken, renderPage, serve, signedIn } from "./session.ts";
import { json } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

type Saved = { body: unknown; csrf: string | null };

function serveSettings(save: (body: unknown) => Response) {
  const saved: Saved[] = [];
  serve(signedIn, async (request) => {
    const { pathname } = new URL(request.url);
    if (pathname !== "/api/v1/settings") throw new Error(pathname);
    if (request.method === "GET")
      return json({ signup: true, passwordSignup: false });
    const body: unknown = await request.json();
    saved.push({ body, csrf: request.headers.get("X-CSRF-Token") });
    return save(body);
  });
  return saved;
}

function checked(name: string) {
  return screen.getByRole("checkbox", { name }).getAttribute("aria-checked");
}

test("settings load and each toggle saves both values", async () => {
  const saved = serveSettings((body) => json(body));
  renderPage(createElement(Settings));
  await screen.findByRole("checkbox", { name: "Open signup" });
  assert.equal(checked("Open signup"), "true");
  assert.equal(checked("Password signup"), "false");

  fireEvent.click(screen.getByRole("checkbox", { name: "Password signup" }));
  await screen.findByText("Settings saved.");
  assert.equal(checked("Password signup"), "true");

  fireEvent.click(screen.getByRole("checkbox", { name: "Open signup" }));
  await waitFor(() => assert.equal(checked("Open signup"), "false"));
  assert.deepEqual(saved, [
    { body: { signup: true, passwordSignup: true }, csrf: csrfToken },
    { body: { signup: false, passwordSignup: true }, csrf: csrfToken },
  ]);
});

test("settings show the saved values the server returns", async () => {
  // The server is the authority; the page shows what it saved.
  serveSettings(() => json({ signup: false, passwordSignup: false }));
  renderPage(createElement(Settings));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "Password signup" }),
  );
  await screen.findByText("Settings saved.");
  assert.equal(checked("Open signup"), "false");
  assert.equal(checked("Password signup"), "false");
});

test("a failed save keeps the previous value and explains why", async () => {
  serveSettings(() =>
    json(
      { title: "Bad Request", status: 400, detail: "Settings refused" },
      400,
    ),
  );
  renderPage(createElement(Settings));
  fireEvent.click(await screen.findByRole("checkbox", { name: "Open signup" }));
  await screen.findByText("Settings refused");
  assert.equal(checked("Open signup"), "true");
  assert.equal(screen.queryByText("Settings saved."), null);
});
