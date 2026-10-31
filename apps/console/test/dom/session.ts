import type { ConsoleBootstrap } from "@diffx/console-api";
import { render } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { SessionProvider, useSession } from "../../src/session.tsx";
import { json, requests } from "./setup.ts";

export const csrfToken = "csrf-token";

export const signedIn: ConsoleBootstrap = {
  setupRequired: false,
  googleEnabled: false,
  admin: { id: "admin-1", username: "root", email: null, method: "password" },
  csrfToken,
};

/** Serves the bootstrap and hands every other request to handler. */
export function serve(
  bootstrap: ConsoleBootstrap,
  handler: (request: Request) => Response | Promise<Response>,
) {
  requests((request) =>
    new URL(request.url).pathname === "/api/v1/bootstrap"
      ? json(bootstrap)
      : handler(request),
  );
}

function Ready({ children }: { children: ReactNode }) {
  const { state } = useSession();
  return state.status === "ready" ? children : null;
}

/** Renders a page once the session has loaded, as the console's routes do. */
export function renderPage(page: ReactNode) {
  return render(
    createElement(SessionProvider, null, createElement(Ready, null, page)),
  );
}
