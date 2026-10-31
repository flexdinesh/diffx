import {
  api,
  errorDetail,
  type ConsoleBootstrap,
  type ConsoleMiddleware,
} from "@diffx/console-api";
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

type SessionState =
  | { status: "loading" }
  | { status: "error"; detail: string }
  | { status: "ready"; bootstrap: ConsoleBootstrap };
interface Session {
  state: SessionState;
  reload: () => void;
  establish: (bootstrap: ConsoleBootstrap) => void;
  signedOut: () => void;
}
const SessionContext = createContext<Session | null>(null);
/** Requests made without a browser session; their 401s are not sign-outs. */
const anonymous = new Set([
  "/api/v1/bootstrap",
  "/api/v1/setup",
  "/api/v1/setup/google",
  "/api/v1/auth/login",
  "/api/v1/auth/google",
  "/api/v1/password-reset/lookup",
  "/api/v1/password-reset",
]);

export function navigate(path: string) {
  window.history.pushState(null, "", path);
  window.dispatchEvent(new PopStateEvent("popstate"));
}

/** The current path, following in-app navigation and history. */
export function usePath() {
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const changed = () => setPath(window.location.pathname);
    window.addEventListener("popstate", changed);
    return () => window.removeEventListener("popstate", changed);
  }, []);
  return path;
}

/**
 * Owns the console's browser session: the bootstrap, the CSRF token on unsafe
 * requests, and sign-out when the server stops accepting the session.
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<SessionState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  const current = useRef<ConsoleBootstrap | null>(null);
  const owner = useRef(new AbortController());
  const establish = useCallback((bootstrap: ConsoleBootstrap) => {
    // Requests of the previous session must not act on the new one.
    owner.current.abort();
    owner.current = new AbortController();
    current.current = bootstrap;
    setState({ status: "ready", bootstrap });
  }, []);
  const signedOut = useCallback(() => {
    const bootstrap = current.current;
    if (!bootstrap) return;
    establish({ ...bootstrap, admin: null, csrfToken: null });
    window.history.replaceState(null, "", "/");
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, [establish]);
  useEffect(() => {
    const middleware: ConsoleMiddleware = {
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
      .GET("/api/v1/bootstrap", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data)
          throw new Error(errorDetail(error, "Unable to load the console"));
        establish(data);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({
            status: "error",
            detail: errorDetail(error, "Unable to load the console"),
          });
      });
    return () => controller.abort();
  }, [attempt, establish]);
  const reload = useCallback(() => setAttempt((value) => value + 1), []);
  return (
    <SessionContext value={{ state, reload, establish, signedOut }}>
      {children}
    </SessionContext>
  );
}

export function useSession() {
  const session = useContext(SessionContext);
  if (!session) throw new Error("Session consumers require SessionProvider");
  return session;
}

/** The signed-in admin; render only inside the signed-in shell. */
export function useAdmin() {
  const { state } = useSession();
  const admin = state.status === "ready" ? state.bootstrap.admin : null;
  if (!admin) throw new Error("Admin consumers require a signed-in admin");
  return admin;
}

export function SessionStatus() {
  const { state, reload } = useSession();
  return (
    <main className="grid h-dvh place-content-center" role="status">
      <EmptyState
        size="page"
        heading="h1"
        title={state.status === "error" ? "Cannot load the console" : "Loading"}
        description={
          state.status === "error" ? state.detail : "Connecting to the console…"
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
