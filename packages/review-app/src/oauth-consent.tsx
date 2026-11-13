import {
  api,
  errorDetail,
  readOAuthRedirect,
  readOAuthRequest,
  type ApiOAuthRequest,
  type ApiOAuthScope,
} from "@diffx/api";
import { CheckIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import { Checkbox } from "@diffx/ui/components/ui/checkbox";
import { AuthPage, prose } from "./account.tsx";
import { navigate, useIdentity } from "./identity-context.tsx";
import { forgetPushDevice } from "./push.ts";
import { consentPage, returnQuery } from "./public-pull-request.ts";

/** The consent page; `/oauth/authorize` renders it only for refused requests. */
export const consentPath = "/oauth/consent";
export const isOAuthPath = (path: string) =>
  path === consentPath || path === "/oauth/authorize";

/** The pending request a consent URL names, as a sign-in return page. */
export function consentReturn(location: { pathname: string; search: string }) {
  if (location.pathname !== consentPath) return null;
  return consentPage(new URLSearchParams(location.search).get("request") ?? "");
}

type RequestState =
  | { status: "loading" }
  | { status: "invalid" }
  | { status: "error"; message: string }
  | { status: "ready"; request: ApiOAuthRequest };

const invalidMessage =
  "This connection request is invalid, expired or already answered. Start connecting again from your app.";

/** A request diffx refused before consent, such as an unknown client. */
export function OAuthRequestInvalid() {
  return (
    <AuthPage title="Can't connect this app">
      <p className={prose}>{invalidMessage}</p>
    </AuthPage>
  );
}

/**
 * Asks the signed-in user to connect an MCP client: who it is, where the
 * browser returns, and what it may do. Reading is always granted; comment
 * writes only when the app asked and the box stays ticked.
 */
export function OAuthConsent() {
  const { state, signedOut } = useIdentity();
  const [request] = useState(
    () => new URLSearchParams(window.location.search).get("request") ?? "",
  );
  const [next] = useState(() => consentReturn(window.location));
  const [current, setCurrent] = useState<RequestState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const [comments, setComments] = useState(true);
  const [busy, setBusy] = useState<"allow" | "deny" | "signout" | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!next) {
      setCurrent({ status: "invalid" });
      return;
    }
    const controller = new AbortController();
    setCurrent({ status: "loading" });
    void api
      .GET("/api/v2/oauth/requests/{requestId}", {
        params: { path: { requestId: request } },
        signal: controller.signal,
      })
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setCurrent({ status: "invalid" });
          return;
        }
        if (!data)
          throw new Error(
            errorDetail(error, "Unable to load the request. Try again."),
          );
        setCurrent({ status: "ready", request: readOAuthRequest(data) });
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setCurrent({
            status: "error",
            message: errorDetail(
              failure,
              "Unable to load the request. Try again.",
            ),
          });
      });
    return () => controller.abort();
  }, [request, next, attempt]);
  if (state.status !== "ready" || !state.bootstrap.user) return null;
  const email = state.bootstrap.user.email;
  async function answer(allow: boolean, offered: ApiOAuthScope[]) {
    setBusy(allow ? "allow" : "deny");
    setError("");
    try {
      const scopes = offered.filter(
        (scope) => scope === "reviews:read" || comments,
      );
      const { data, error, response } = await api.POST(
        "/api/v2/oauth/requests/{requestId}",
        { params: { path: { requestId: request } }, body: { allow, scopes } },
      );
      if (response.status === 404) {
        setCurrent({ status: "invalid" });
        setBusy(null);
        return;
      }
      if (!data)
        throw new Error(
          errorDetail(error, "Unable to answer the request. Try again."),
        );
      // The client's page takes over; stay busy until the browser leaves.
      window.location.assign(readOAuthRedirect(data));
    } catch (failure: unknown) {
      setError(
        errorDetail(failure, "Unable to answer the request. Try again."),
      );
      setBusy(null);
    }
  }
  async function signOut() {
    setBusy("signout");
    setError("");
    try {
      await forgetPushDevice();
      const { response, error } = await api.POST("/api/v2/auth/logout");
      if (!response.ok)
        throw new Error(errorDetail(error, "Unable to sign out. Try again."));
      signedOut();
      navigate(`/signin${returnQuery(next)}`, { replace: true });
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to sign out. Try again."));
      setBusy(null);
    }
  }
  if (current.status === "invalid") return <OAuthRequestInvalid />;
  if (current.status !== "ready")
    return (
      <AuthPage title="Connect an app to diffx">
        {current.status === "loading" ? (
          <p className={prose} role="status">
            Checking the request…
          </p>
        ) : (
          <ErrorState
            message={current.message}
            touch
            onRetry={() => setAttempt((value) => value + 1)}
          />
        )}
      </AuthPage>
    );
  const { client, redirectHost, scopes } = current.request;
  const writes = scopes.includes("comments:write");
  return (
    <AuthPage title={`Connect ${client.name} to diffx`}>
      <p className="mb-2 flex flex-wrap items-center gap-2 leading-copy">
        {client.verified ? (
          <>
            <Badge variant="status" tone="success">
              Verified
            </Badge>
            <span className="wrap-anywhere">{client.host}</span>
          </>
        ) : (
          <>
            <Badge variant="status" tone="warning">
              Unverified app
            </Badge>
            <span className="text-secondary">Name provided by the app</span>
          </>
        )}
      </p>
      <p className={`${prose} wrap-anywhere`}>
        Will redirect to <strong>{redirectHost}</strong>
      </p>
      <h2 className="mb-2 text-md font-semibold">It will be able to</h2>
      <ul className="mb-6 grid gap-1" aria-label="Access">
        <li className="flex min-h-control items-center gap-2 max-md:min-h-touch pointer-coarse:min-h-touch">
          <span className="flex size-icon items-center justify-center text-success">
            <CheckIcon aria-hidden="true" className="size-icon-sm" />
          </span>
          Read your reviews, diffs and comments
        </li>
        {writes && (
          <li className="flex min-h-control items-center gap-2 max-md:min-h-touch pointer-coarse:min-h-touch">
            <Checkbox
              id="consent-comments"
              checked={comments}
              disabled={busy !== null}
              onCheckedChange={setComments}
            />
            <label
              htmlFor="consent-comments"
              className="flex items-center self-stretch"
            >
              Add comments, reply and resolve
            </label>
          </li>
        )}
      </ul>
      {error && <ErrorState message={error} className="mb-4" />}
      <div className="mb-6 flex flex-wrap gap-2">
        <Button
          touch
          disabled={busy !== null}
          aria-busy={busy === "allow"}
          onClick={() => void answer(true, scopes)}
        >
          Allow
        </Button>
        <Button
          variant="outline"
          touch
          disabled={busy !== null}
          aria-busy={busy === "deny"}
          onClick={() => void answer(false, scopes)}
        >
          Cancel
        </Button>
      </div>
      <p className="text-sm leading-copy text-secondary wrap-anywhere">
        Signed in as <strong>{email}</strong>.{" "}
        <Button
          variant="link"
          size="inline"
          touch
          disabled={busy !== null}
          onClick={() => void signOut()}
        >
          Sign out
        </Button>
      </p>
    </AuthPage>
  );
}
