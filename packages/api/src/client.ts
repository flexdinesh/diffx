import createClient from "openapi-fetch";
export type { Middleware as ApiMiddleware } from "openapi-fetch";
import type { components, paths } from "./schema.d.ts";

export function createApiClient({ baseUrl }: { baseUrl?: string } = {}) {
  return createClient<paths>({
    ...(baseUrl === undefined ? {} : { baseUrl }),
  });
}

export const api = createApiClient();

export type ApiContext = components["schemas"]["Context"];
export type ApiContextPage = components["schemas"]["ContextPage"];
export type ApiSession = components["schemas"]["Session"];
export type ApiServerMetrics = components["schemas"]["ServerMetrics"];
export type ApiRepositoryDiff = components["schemas"]["RepositoryDiff"];
export type ApiChangedFile = components["schemas"]["ChangedFile"];
export type ApiFilePatch = components["schemas"]["FilePatch"];
export type ApiFileContents = components["schemas"]["FileContents"];
export type ApiReviewComment = components["schemas"]["ReviewComment"];
export type ApiReviewMark = components["schemas"]["ReviewMark"];
export type ApiCollectionResult = components["schemas"]["CollectionResult"];
export type ApiCollectionFailure = components["schemas"]["CollectionFailure"];

export function errorDetail(error: unknown, fallback: string) {
  if (error instanceof Error) return error.message;
  return typeof error === "object" &&
    error !== null &&
    "detail" in error &&
    typeof error.detail === "string"
    ? error.detail
    : fallback;
}

export type ApiBootstrap = components["schemas"]["Bootstrap"];
export type ApiAccountToken = components["schemas"]["AccountToken"];

function object(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

export function readBootstrap(value: unknown): ApiBootstrap {
  if (!object(value)) throw new Error("Invalid server identity response");
  if (
    value.mode === "local" &&
    value.signupEnabled === false &&
    value.user === null &&
    value.csrfToken === null &&
    typeof value.collect === "boolean"
  )
    return {
      mode: "local",
      signupEnabled: false,
      user: null,
      csrfToken: null,
      collect: value.collect,
    };
  if (value.mode !== "remote" || typeof value.signupEnabled !== "boolean")
    throw new Error("Invalid server identity response");
  if (value.user === null && value.csrfToken === null)
    return {
      mode: "remote",
      signupEnabled: value.signupEnabled,
      user: null,
      csrfToken: null,
    };
  if (
    object(value.user) &&
    typeof value.user.id === "string" &&
    value.user.id.length > 0 &&
    typeof value.user.username === "string" &&
    value.user.username.length > 0 &&
    typeof value.csrfToken === "string" &&
    value.csrfToken.length > 0
  )
    return {
      mode: "remote",
      signupEnabled: value.signupEnabled,
      user: { id: value.user.id, username: value.user.username },
      csrfToken: value.csrfToken,
    };
  throw new Error("Invalid server identity response");
}

export function readAccountToken(value: unknown): ApiAccountToken {
  if (
    !object(value) ||
    typeof value.id !== "string" ||
    typeof value.name !== "string" ||
    (value.purpose !== "collector" && value.purpose !== "api") ||
    typeof value.createdAt !== "number" ||
    !Number.isSafeInteger(value.createdAt) ||
    typeof value.expiresAt !== "number" ||
    !Number.isSafeInteger(value.expiresAt) ||
    (value.lastUsedAt !== null &&
      (typeof value.lastUsedAt !== "number" ||
        !Number.isSafeInteger(value.lastUsedAt)))
  )
    throw new Error("Invalid token response");
  return {
    id: value.id,
    name: value.name,
    purpose: value.purpose,
    createdAt: value.createdAt,
    expiresAt: value.expiresAt,
    lastUsedAt: value.lastUsedAt,
  };
}

export function readAccountTokens(value: unknown): ApiAccountToken[] {
  if (!object(value) || !Array.isArray(value.tokens))
    throw new Error("Invalid token response");
  return value.tokens.map(readAccountToken);
}

export function readCreatedToken(value: unknown): {
  token: ApiAccountToken;
  secret: string;
} {
  if (
    !object(value) ||
    typeof value.secret !== "string" ||
    value.secret.length === 0
  )
    throw new Error("Invalid token response");
  return { token: readAccountToken(value.token), secret: value.secret };
}
