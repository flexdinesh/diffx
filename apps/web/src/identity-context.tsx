import {
  api,
  errorDetail,
  readBootstrap,
  type ApiBootstrap,
  type ApiMiddleware,
} from "@diffx/api";
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { Button } from "@diffx/ui/components/ui/button";

type IdentityState =
  | { status: "loading" }
  | { status: "error"; detail: string }
  | { status: "ready"; bootstrap: ApiBootstrap };
interface Identity {
  state: IdentityState;
  reload: () => void;
  establish: (value: unknown) => void;
  signedOut: () => void;
}
const IdentityContext = createContext<Identity | null>(null);
/** Requests made without a browser session; their 401s are not sign-outs. */
const anonymous = new Set([
  "/api/v2/bootstrap",
  "/api/v2/auth/login",
  "/api/v2/setup",
  "/api/v2/auth/google",
  "/api/v2/invites/lookup",
  "/api/v2/invites/google",
]);

export function navigate(path: string) {
  window.history.pushState(null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

export function IdentityProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<IdentityState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const current = useRef<ApiBootstrap | null>(null);
  const owner = useRef(new AbortController());
  const establish = useCallback((value: unknown) => {
    const bootstrap = readBootstrap(value);
    if (current.current && current.current.mode !== bootstrap.mode)
      throw new Error("Server deployment mode changed. Reload this page.");
    owner.current.abort();
    owner.current = new AbortController();
    current.current = bootstrap;
    setState({ status: "ready", bootstrap });
  }, []);
  const signedOut = useCallback(() => {
    const bootstrap = current.current;
    if (!bootstrap || bootstrap.mode !== "remote") return;
    establish({ ...bootstrap, user: null, csrfToken: null });
    window.history.replaceState(null, "", "/");
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, [establish]);
  useEffect(() => {
    const middleware: ApiMiddleware = {
      onRequest({ request }) {
        if (anonymous.has(new URL(request.url).pathname)) return request;
        const csrf = current.current?.csrfToken;
        if (csrf && request.method !== "GET" && request.method !== "HEAD")
          request.headers.set("X-CSRF-Token", csrf);
        return new Request(request, {
          signal: AbortSignal.any([request.signal, owner.current.signal]),
        });
      },
      onResponse({ request, response }) {
        if (
          response.status === 401 &&
          !request.signal.aborted &&
          !anonymous.has(new URL(request.url).pathname)
        )
          signedOut();
      },
    };
    api.use(middleware);
    return () => {
      api.eject(middleware);
      owner.current.abort();
    };
  }, [signedOut]);
  useEffect(() => {
    const controller = new AbortController();
    setState({ status: "loading" });
    void api
      .GET("/api/v2/bootstrap", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data)
          throw new Error(errorDetail(error, "Unable to load server identity"));
        establish(data);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({
            status: "error",
            detail: errorDetail(error, "Unable to load server identity"),
          });
      });
    return () => controller.abort();
  }, [attempt, establish]);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);
  return (
    <IdentityContext value={{ state, reload, establish, signedOut }}>
      {children}
    </IdentityContext>
  );
}

export function useIdentity() {
  const identity = useContext(IdentityContext);
  if (!identity) throw new Error("Identity consumers require IdentityProvider");
  return identity;
}

export function IdentityStatus() {
  const { state, reload } = useIdentity();
  return (
    <main className="grid h-dvh place-content-center" role="status">
      <EmptyState
        size="page"
        heading="h1"
        title={state.status === "error" ? "Cannot load diffx" : "Loading diffx"}
        description={
          state.status === "error" ? state.detail : "Connecting to your server…"
        }
        action={
          state.status === "error" ? (
            <Button variant="outline" onClick={reload}>
              Retry
            </Button>
          ) : undefined
        }
      />
    </main>
  );
}
