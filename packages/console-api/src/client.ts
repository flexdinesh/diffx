import createClient from "openapi-fetch";
export type { Middleware as ConsoleMiddleware } from "openapi-fetch";
import type { components, paths } from "./schema.d.ts";

export function createConsoleClient({ baseUrl }: { baseUrl?: string } = {}) {
  return createClient<paths>({
    ...(baseUrl === undefined ? {} : { baseUrl }),
  });
}

export const api = createConsoleClient();

export type ConsoleBootstrap = components["schemas"]["Bootstrap"];
export type ConsoleAdmin = components["schemas"]["Admin"];
export type ConsoleAccount = components["schemas"]["Account"];
export type ConsoleInvite = components["schemas"]["Invite"];
export type ConsoleSettings = components["schemas"]["Settings"];
export type ConsoleMethod = components["schemas"]["Method"];

/** The problem detail of a failed request, or fallback. */
export function errorDetail(error: unknown, fallback: string) {
  if (error instanceof Error) return error.message;
  return typeof error === "object" &&
    error !== null &&
    "detail" in error &&
    typeof error.detail === "string"
    ? error.detail
    : fallback;
}
