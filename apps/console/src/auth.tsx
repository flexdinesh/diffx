import { api, errorDetail, type ConsoleBootstrap } from "@diffx/console-api";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import { Separator } from "@diffx/ui/components/ui/separator";
import { navigate, useSession } from "./session.tsx";

const prose = "mb-4 leading-copy text-secondary";

/** Failed outcomes the Google callback reports through `/?signin=`. */
const signInFailures: Record<string, string> = {
  "no-account": "That Google account is not an admin of this server.",
  failed: "Google sign-in failed. Try again.",
};

/** Reads and clears the callback outcome so a reload does not repeat it. */
function takeSignInOutcome() {
  const url = new URL(window.location.href);
  const outcome = url.searchParams.get("signin");
  if (outcome === null) return "";
  url.searchParams.delete("signin");
  window.history.replaceState(window.history.state, "", url);
  return outcome;
}

/** The signed-out page shell shared by sign-in, setup and reset. */
function AuthPage({ title, children }: { title: string; children: ReactNode }) {
  return (
    <main className="console-page auth-page">
      <a className="brand" href="/">
        diffx <span className="brand-product">Console</span>
      </a>
      <h1 className="mb-4 text-xl font-semibold">{title}</h1>
      {children}
    </main>
  );
}

/** Admin sign-in: password always, Google when the console configures it. */
export function SignIn() {
  const { state } = useSession();
  const bootstrap = state.status === "ready" ? state.bootstrap : null;
  const google = bootstrap?.googleEnabled === true;
  const [outcome] = useState(takeSignInOutcome);
  const failure = signInFailures[outcome];
  return (
    <AuthPage title="Sign in">
      {bootstrap?.setupRequired ? (
        <p className={prose}>
          This server has no admin yet. Open the setup link diffx-console wrote
          to its log at startup.
        </p>
      ) : (
        <p className={prose}>Admins of this diffx server sign in here.</p>
      )}
      {outcome === "cancelled" && (
        <p className={prose} role="status">
          Google sign-in was cancelled.
        </p>
      )}
      {failure && <ErrorState message={failure} className="mb-4" />}
      {google && (
        <>
          <ContinueWithGoogle
            start={() => api.POST("/api/v1/auth/google", { body: {} })}
          />
          <Separator className="my-6" />
        </>
      )}
      <CredentialForm
        submitLabel="Sign in"
        failure="Unable to sign in. Check your email and password."
        submit={(credentials) =>
          api.POST("/api/v1/auth/login", {
            body: credentials,
          })
        }
      />
    </AuthPage>
  );
}

/**
 * Google sits beside the password form, so it is the secondary action. A 404
 * reports `notFound` when given.
 */
function ContinueWithGoogle({
  start,
  notFound,
}: {
  start: () => Promise<{
    data?: { authorizeUrl: string };
    error?: unknown;
    response: Response;
  }>;
  notFound?: string;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function begin() {
    setBusy(true);
    setError("");
    try {
      const { data, error, response } = await start();
      if (notFound && response.status === 404) throw new Error(notFound);
      if (!data)
        throw new Error(
          errorDetail(error, "Unable to start Google sign-in. Try again."),
        );
      window.location.assign(data.authorizeUrl);
    } catch (failure: unknown) {
      setError(
        errorDetail(failure, "Unable to start Google sign-in. Try again."),
      );
      setBusy(false);
    }
  }
  return (
    <>
      {error && <ErrorState message={error} className="mb-4" />}
      <Button
        touch
        variant="outline"
        disabled={busy}
        aria-busy={busy}
        onClick={() => void begin()}
      >
        Continue with Google
      </Button>
    </>
  );
}

/**
 * Reads a link secret from the fragment once and drops it from history, so it
 * never reaches a request URL or the back button.
 */
function useFragmentToken(path: string) {
  const [token, setToken] = useState(
    () => new URLSearchParams(window.location.hash.slice(1)).get("t") ?? "",
  );
  useEffect(() => {
    window.history.replaceState(null, "", path);
    const changed = () => {
      const next =
        new URLSearchParams(window.location.hash.slice(1)).get("t") ?? "";
      window.history.replaceState(null, "", path);
      if (next) setToken(next);
    };
    window.addEventListener("hashchange", changed);
    return () => window.removeEventListener("hashchange", changed);
  }, [path]);
  return token;
}

/** Creates the first admin from the setup link diffx-console logged. */
export function Setup() {
  const token = useFragmentToken("/setup");
  const { state } = useSession();
  const google = state.status === "ready" && state.bootstrap.googleEnabled;
  return (
    <AuthPage title="Set up the console">
      {token ? (
        <>
          <p className={prose}>
            {google
              ? "Create the first admin with Google or as a password account. It also signs in to the diffx app, whatever its settings."
              : "Create the first admin. It is a password account that also signs in to the diffx app, whatever its settings."}
          </p>
          {google && (
            <>
              <ContinueWithGoogle
                start={() =>
                  api.POST("/api/v1/setup/google", { body: { token } })
                }
                notFound="This setup link is invalid or expired, or Google is not configured. Open the setup link diffx-console wrote to its log at startup; a restart writes a new one."
              />
              <Separator className="my-6" />
            </>
          )}
          <CredentialForm
            creating
            submitLabel="Create admin"
            failure="Unable to create the admin. Try again."
            submit={(credentials) =>
              api.POST("/api/v1/setup", {
                body: { ...credentials, token },
              })
            }
          />
        </>
      ) : (
        <p className={prose}>
          Open the setup link diffx-console wrote to its log at startup. A
          restart writes a new one.
        </p>
      )}
    </AuthPage>
  );
}

type ResetLink =
  | { status: "loading" }
  | { status: "invalid" }
  | { status: "error"; message: string }
  | { status: "ready"; email: string };

/** Resolves a reset link; 404 means invalid, expired or used. */
function useResetLink(token: string) {
  const [link, setLink] = useState<ResetLink>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!token) {
      setLink({ status: "invalid" });
      return;
    }
    const controller = new AbortController();
    const failure = "Unable to load the reset link";
    setLink({ status: "loading" });
    void api
      .POST("/api/v1/password-reset/lookup", {
        body: { token },
        signal: controller.signal,
      })
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setLink({ status: "invalid" });
          return;
        }
        if (!data) throw new Error(errorDetail(error, failure));
        setLink({ status: "ready", email: data.email });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setLink({ status: "error", message: errorDetail(error, failure) });
      });
    return () => controller.abort();
  }, [token, attempt]);
  return { link, retry: () => setAttempt((value) => value + 1) };
}

/** Sets an admin's new password from a reset link. */
export function ResetPassword() {
  const token = useFragmentToken("/reset-password");
  const { link, retry } = useResetLink(token);
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [done, setDone] = useState(false);
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { response, error } = await api.POST("/api/v1/password-reset", {
        body: { token, password },
      });
      if (!response.ok)
        throw new Error(
          errorDetail(error, "Unable to set the password. Try again."),
        );
      setPassword("");
      setDone(true);
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to set the password. Try again."));
    } finally {
      setBusy(false);
    }
  }
  return (
    <AuthPage title="Reset password">
      {done ? (
        <p className={prose} role="status">
          Your password is set and your browser sessions are signed out.{" "}
          <a
            href="/"
            className="text-accent underline underline-offset-2"
            onClick={(event) => {
              event.preventDefault();
              navigate("/");
            }}
          >
            Sign in
          </a>{" "}
          with the new password.
        </p>
      ) : (
        <>
          {link.status === "loading" && (
            <p className={prose} role="status">
              Checking your reset link…
            </p>
          )}
          {link.status === "error" && (
            <ErrorState
              message={link.message}
              className="mb-4"
              touch
              onRetry={retry}
            />
          )}
          {link.status === "invalid" && (
            <p className={prose}>
              This reset link is invalid, expired or already used. Ask another
              admin for a new one, or run diffx-console user reset-password.
            </p>
          )}
          {link.status === "ready" && (
            <>
              <p className={prose}>
                Choose a new password for <strong>{link.email}</strong>. This
                signs out your browser sessions in the console and the app; API
                tokens remain active.
              </p>
              <form className="console-form" onSubmit={submit}>
                {/* Lets password managers save the new password for this account. */}
                <Input
                  type="text"
                  name="email"
                  autoComplete="username"
                  value={link.email}
                  readOnly
                  hidden
                />
                <label htmlFor="new-password">New password</label>
                <Input
                  id="new-password"
                  type="password"
                  autoComplete="new-password"
                  touch
                  minLength={8}
                  maxLength={128}
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  required
                  disabled={busy}
                  aria-describedby="new-password-hint"
                />
                <p
                  id="new-password-hint"
                  className="text-sm leading-copy text-secondary"
                >
                  Use at least 8 characters.
                </p>
                {error && <ErrorState message={error} />}
                <Button
                  type="submit"
                  touch
                  className="mt-2 justify-self-start"
                  disabled={busy}
                  aria-busy={busy}
                >
                  Set password
                </Button>
              </form>
            </>
          )}
        </>
      )}
    </AuthPage>
  );
}

type Credentials = { email: string; password: string };

/** Email and password sign-in or admin creation. */
function CredentialForm({
  creating = false,
  submitLabel,
  failure,
  submit,
}: {
  creating?: boolean;
  submitLabel: string;
  failure: string;
  submit: (credentials: Credentials) => Promise<{
    data?: ConsoleBootstrap;
    error?: unknown;
    response: Response;
  }>;
}) {
  const { establish } = useSession();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function send(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { data, error, response } = await submit({ email, password });
      // Refused credentials share one generic 401; name the form's failure.
      if (response.status === 401) throw new Error(failure);
      if (!data) throw new Error(errorDetail(error, failure));
      if (!data.admin || !data.csrfToken)
        throw new Error(
          "Console did not establish a browser session. Try again.",
        );
      setPassword("");
      window.history.replaceState(null, "", "/");
      establish(data);
    } catch (failed: unknown) {
      setError(errorDetail(failed, failure));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="console-form" onSubmit={send}>
      <label htmlFor="email">Email</label>
      <Input
        id="email"
        name="email"
        type="email"
        autoComplete="username"
        touch
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        required
        minLength={3}
        maxLength={254}
        disabled={busy}
      />
      <label htmlFor="password">Password</label>
      <Input
        id="password"
        name="password"
        type="password"
        autoComplete={creating ? "new-password" : "current-password"}
        touch
        value={password}
        onChange={(event) => setPassword(event.target.value)}
        required
        minLength={creating ? 8 : undefined}
        maxLength={128}
        disabled={busy}
        aria-describedby={creating ? "password-hint" : undefined}
      />
      {creating && (
        <p id="password-hint" className="text-sm leading-copy text-secondary">
          Use at least 8 characters.
        </p>
      )}
      {error && <ErrorState message={error} />}
      <Button
        type="submit"
        touch
        className="mt-2 justify-self-start"
        disabled={busy}
        aria-busy={busy}
      >
        {submitLabel}
      </Button>
    </form>
  );
}
