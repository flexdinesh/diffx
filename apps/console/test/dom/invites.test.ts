import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { createElement } from "react";
import { Invites } from "../../src/invites.tsx";
import { csrfToken, renderPage, serve, signedIn } from "./session.ts";
import { json } from "./setup.ts";

afterEach(() => {
  cleanup();
  window.history.replaceState(null, "", "/");
});

test("invites add members and post only the email", async () => {
  const created: { body: unknown; csrf: string | null }[] = [];
  serve(signedIn, async (request) => {
    const { pathname } = new URL(request.url);
    if (pathname !== "/api/v1/invites") throw new Error(pathname);
    if (request.method === "GET") return json({ invites: [] });
    created.push({
      body: await request.json(),
      csrf: request.headers.get("X-CSRF-Token"),
    });
    return json(
      {
        invite: {
          id: "invite-1",
          email: "dana@example.com",
          status: "pending",
          createdAt: 1,
          expiresAt: 2,
          usedBy: null,
        },
        url: "http://app.test/invite#t=secret",
      },
      201,
    );
  });
  renderPage(createElement(Invites));
  await screen.findByText("No invites yet.");
  // Admins come from promoting accounts, never from invites.
  assert.equal(screen.queryByRole("checkbox"), null);
  assert.equal(screen.queryByText(/admin of this console/), null);

  fireEvent.change(screen.getByLabelText("Email"), {
    target: { value: "dana@example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create invite" }));
  await screen.findByText("Send this invite link");
  assert.deepEqual(created, [
    { body: { email: "dana@example.com" }, csrf: csrfToken },
  ]);
  screen.getByText(/It works once, for dana@example\.com, until/);
  assert.equal(screen.queryByText(/as an admin/), null);
});
