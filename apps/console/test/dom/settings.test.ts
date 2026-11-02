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

const loaded = {
  signup: true,
  passwordSignup: false,
  publicPullRequests: false,
  publicRetentionDays: 7,
};

function serveSettings(save: (body: unknown) => Response) {
  const saved: Saved[] = [];
  serve(signedIn, async (request) => {
    const { pathname } = new URL(request.url);
    if (pathname !== "/api/v1/settings") throw new Error(pathname);
    if (request.method === "GET") return json(loaded);
    const body: unknown = await request.json();
    saved.push({ body, csrf: request.headers.get("X-CSRF-Token") });
    return save(body);
  });
  return saved;
}

function checked(name: string) {
  return screen.getByRole("checkbox", { name }).getAttribute("aria-checked");
}

function retention() {
  return screen.getByRole("spinbutton", { name: "Public retention (days)" });
}

function retentionValue() {
  const input = retention();
  return input instanceof HTMLInputElement ? input.value : null;
}

test("settings load and each toggle saves every value", async () => {
  const saved = serveSettings((body) => json(body));
  renderPage(createElement(Settings));
  await screen.findByRole("checkbox", { name: "Open signup" });
  assert.equal(checked("Open signup"), "true");
  assert.equal(checked("Password signup"), "false");
  assert.equal(checked("Public pull requests"), "false");
  assert.equal(retentionValue(), "7");

  fireEvent.click(screen.getByRole("checkbox", { name: "Password signup" }));
  await screen.findByText("Settings saved.");
  assert.equal(checked("Password signup"), "true");

  fireEvent.click(screen.getByRole("checkbox", { name: "Open signup" }));
  await waitFor(() => assert.equal(checked("Open signup"), "false"));

  fireEvent.click(
    screen.getByRole("checkbox", { name: "Public pull requests" }),
  );
  await waitFor(() => assert.equal(checked("Public pull requests"), "true"));
  const rest = { publicRetentionDays: 7 };
  assert.deepEqual(saved, [
    {
      body: {
        signup: true,
        passwordSignup: true,
        publicPullRequests: false,
        ...rest,
      },
      csrf: csrfToken,
    },
    {
      body: {
        signup: false,
        passwordSignup: true,
        publicPullRequests: false,
        ...rest,
      },
      csrf: csrfToken,
    },
    {
      body: {
        signup: false,
        passwordSignup: true,
        publicPullRequests: true,
        ...rest,
      },
      csrf: csrfToken,
    },
  ]);
});

test("saving retention sends every value and shows the saved days", async () => {
  const saved = serveSettings((body) => json(body));
  renderPage(createElement(Settings));
  await screen.findByRole("spinbutton", { name: "Public retention (days)" });

  for (const days of [1, 30]) {
    fireEvent.change(retention(), { target: { value: String(days) } });
    fireEvent.click(screen.getByRole("button", { name: "Save retention" }));
    await waitFor(() => assert.equal(saved.length, days === 1 ? 1 : 2));
    await screen.findByText("Settings saved.");
    assert.equal(retentionValue(), String(days));
  }
  assert.deepEqual(
    saved.map((entry) => entry.body),
    [
      { ...loaded, publicRetentionDays: 1 },
      { ...loaded, publicRetentionDays: 30 },
    ],
  );
  assert.equal(saved[0]?.csrf, csrfToken);
});

test("retention outside 1 to 30 days is refused before saving", async () => {
  const saved = serveSettings((body) => json(body));
  renderPage(createElement(Settings));
  await screen.findByRole("spinbutton", { name: "Public retention (days)" });
  const message = "Enter a whole number of days from 1 to 30.";

  for (const value of ["0", "31", "2.5", ""]) {
    fireEvent.change(retention(), { target: { value } });
    assert.equal(retention().getAttribute("aria-invalid"), "false");
    fireEvent.click(screen.getByRole("button", { name: "Save retention" }));
    const alert = await screen.findByText(message);
    assert.equal(retention().getAttribute("aria-invalid"), "true");
    assert.ok(
      retention()
        .getAttribute("aria-describedby")
        ?.split(" ")
        .includes(alert.id),
    );
  }
  assert.deepEqual(saved, []);

  // Fixing the value clears the error.
  fireEvent.change(retention(), { target: { value: "14" } });
  assert.equal(screen.queryByText(message), null);
  assert.equal(retention().getAttribute("aria-invalid"), "false");
});

test("an unsaved retention edit survives a toggle save", async () => {
  const saved = serveSettings((body) => json(body));
  renderPage(createElement(Settings));
  await screen.findByRole("spinbutton", { name: "Public retention (days)" });
  fireEvent.change(retention(), { target: { value: "20" } });
  fireEvent.click(
    screen.getByRole("checkbox", { name: "Public pull requests" }),
  );
  await screen.findByText("Settings saved.");
  assert.equal(retentionValue(), "20");
  assert.deepEqual(
    saved.map((entry) => entry.body),
    [{ ...loaded, publicPullRequests: true }],
  );
});

test("settings show the saved values the server returns", async () => {
  // The server is the authority; the page shows what it saved.
  serveSettings(() =>
    json({
      signup: false,
      passwordSignup: false,
      publicPullRequests: true,
      publicRetentionDays: 3,
    }),
  );
  renderPage(createElement(Settings));
  fireEvent.click(
    await screen.findByRole("checkbox", { name: "Password signup" }),
  );
  await screen.findByText("Settings saved.");
  assert.equal(checked("Open signup"), "false");
  assert.equal(checked("Password signup"), "false");
  assert.equal(checked("Public pull requests"), "true");
  assert.equal(retentionValue(), "3");
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
