import { api, errorDetail, readBootstrap } from "@diffx/api";
import { ArrowLeftIcon, UserRoundIcon } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { ErrorState } from "@/components/error-state";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { navigate, useIdentity } from "./identity-context.tsx";
import { AccountGitHub } from "./account-github.tsx";
import { AccountTokens } from "./account-tokens.tsx";

/** Rail placement on the panel, or the compact header on the shell canvas. */
export function AccountButton({ variant }: { variant: "rail" | "shell" }) {
  const { state } = useIdentity();
  if (
    state.status !== "ready" ||
    state.bootstrap.mode !== "remote" ||
    !state.bootstrap.user
  )
    return null;
  return (
    <IconButton
      variant={variant}
      touch
      label="Account settings"
      hint={`Account settings · ${state.bootstrap.user.username}`}
      onClick={() => navigate("/settings")}
    >
      <UserRoundIcon aria-hidden="true" />
    </IconButton>
  );
}

/** Members sign in with Google; accounts come only from invites. */
export function SignIn() {
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Sign in</h1>
      <p className="mb-4 leading-copy text-secondary">
        Sign-in is not available on this server yet. Ask its admin for an
        invite.
      </p>
    </main>
  );
}

/** Password sign-in for the admin only, at the unlinked /admin/login. */
export function AdminSignIn() {
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Admin sign in</h1>
      <p className="mb-4 leading-copy text-secondary">
        Manage this server and review your collected changes.
      </p>
      <CredentialForm
        path="/api/v2/auth/login"
        token=""
        submitLabel="Sign in"
        failure="Unable to sign in. Try again."
      />
    </main>
  );
}

/** One-time admin creation from the setup link the server logged at startup. */
export function AdminSetup() {
  const [token] = useState(
    () => new URLSearchParams(window.location.hash.slice(1)).get("t") ?? "",
  );
  const [setup, setSetup] = useState<"loading" | "required" | "complete">(
    "loading",
  );
  const [error, setError] = useState("");
  useEffect(() => {
    // The secret stays out of history once read.
    window.history.replaceState(null, "", "/setup");
    const controller = new AbortController();
    void api
      .GET("/api/v2/setup", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data)
          throw new Error(errorDetail(error, "Unable to load setup status"));
        setSetup(data.required ? "required" : "complete");
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(errorDetail(failure, "Unable to load setup status"));
      });
    return () => controller.abort();
  }, []);
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Set up diffx</h1>
      {error && <ErrorState message={error} />}
      {setup === "complete" && (
        <p className="mb-4 leading-copy text-secondary">
          This server already has an admin.{" "}
          <a href="/admin/login">Sign in as admin</a>.
        </p>
      )}
      {setup === "required" && !token && (
        <p className="mb-4 leading-copy text-secondary">
          Open the setup link from the server log to create the admin account.
        </p>
      )}
      {setup === "required" && token && (
        <>
          <p className="mb-4 leading-copy text-secondary">
            Create the admin account. It signs in with this password and invites
            everyone else.
          </p>
          <CredentialForm
            path="/api/v2/setup"
            token={token}
            submitLabel="Create admin"
            failure="Unable to create the admin. Try again."
          />
        </>
      )}
    </main>
  );
}

function CredentialForm({
  path,
  token,
  submitLabel,
  failure,
}: {
  path: "/api/v2/auth/login" | "/api/v2/setup";
  token: string;
  submitLabel: string;
  failure: string;
}) {
  const { establish } = useIdentity();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const creating = path === "/api/v2/setup";
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { data, error } = creating
        ? await api.POST(path, { body: { token, username, password } })
        : await api.POST(path, { body: { username, password } });
      if (!data) throw new Error(errorDetail(error, failure));
      const bootstrap = readBootstrap(data);
      if (
        bootstrap.mode !== "remote" ||
        !bootstrap.user ||
        !bootstrap.csrfToken
      )
        throw new Error(
          "Server did not establish a browser session. Try again.",
        );
      establish(bootstrap);
      setPassword("");
    } catch (failed: unknown) {
      setError(errorDetail(failed, failure));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="account-form" onSubmit={submit}>
      <label htmlFor="username">Username</label>
      <Input
        id="username"
        name="username"
        autoComplete="username"
        touch
        value={username}
        onChange={(event) => setUsername(event.target.value)}
        required
        minLength={3}
        maxLength={32}
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
        minLength={creating ? 12 : undefined}
        maxLength={128}
        disabled={busy}
      />
      {creating && (
        <p className="text-sm leading-copy text-secondary">
          Use at least 12 characters.
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

export function AccountSettings() {
  const { state, signedOut } = useIdentity();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  if (state.status !== "ready" || !state.bootstrap.user) return null;
  async function logout() {
    setBusy(true);
    setError("");
    try {
      const { response, error } = await api.POST("/api/v2/auth/logout");
      if (!response.ok)
        throw new Error(errorDetail(error, "Unable to sign out. Try again."));
      signedOut();
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to sign out. Try again."));
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="account-shell">
      <header className="topbar account-topbar">
        <Button variant="shell" onClick={() => navigate("/")}>
          <ArrowLeftIcon aria-hidden="true" /> Reviews
        </Button>
        <span className="header-actions">{state.bootstrap.user.username}</span>
      </header>
      <main className="account-page settings-page">
        <h1 className="mb-2 text-xl font-semibold">Account settings</h1>
        <p className="mb-4 leading-copy text-secondary">
          Signed in as <strong>{state.bootstrap.user.username}</strong>.
        </p>
        <AccountTokens />
        <AccountGitHub />
        {state.bootstrap.user.admin && <PasswordSettings />}
        <section
          className="mt-8 border-t border-separator pt-6"
          aria-labelledby="session-title"
        >
          <h2 id="session-title" className="mb-2 text-md font-semibold">
            Browser session
          </h2>
          {error && <ErrorState message={error} className="mb-4" />}
          <Button
            variant="outline"
            touch
            disabled={busy}
            aria-busy={busy}
            onClick={() => void logout()}
          >
            Sign out
          </Button>
        </section>
      </main>
    </div>
  );
}

function PasswordSettings() {
  const { signedOut } = useIdentity();
  const [currentPassword, setCurrent] = useState("");
  const [newPassword, setNew] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { response, error } = await api.PUT("/api/v2/account/password", {
        body: { currentPassword, newPassword },
      });
      if (!response.ok)
        throw new Error(
          errorDetail(error, "Unable to change password. Try again."),
        );
      signedOut();
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to change password. Try again."));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="mt-8 border-t border-separator pt-6"
      aria-labelledby="password-title"
    >
      <h2 id="password-title" className="mb-2 text-md font-semibold">
        Password
      </h2>
      <p className="mb-4 leading-copy text-secondary">
        Changing your password signs out every browser session. API tokens
        remain active.
      </p>
      <form className="account-form" onSubmit={submit}>
        <label htmlFor="current-password">Current password</label>
        <Input
          id="current-password"
          type="password"
          autoComplete="current-password"
          touch
          value={currentPassword}
          onChange={(event) => setCurrent(event.target.value)}
          required
          disabled={busy}
        />
        <label htmlFor="new-password">New password</label>
        <Input
          id="new-password"
          type="password"
          autoComplete="new-password"
          touch
          minLength={12}
          maxLength={128}
          value={newPassword}
          onChange={(event) => setNew(event.target.value)}
          required
          disabled={busy}
        />
        <p className="text-sm leading-copy text-secondary">
          Use at least 12 characters.
        </p>
        {error && <ErrorState message={error} />}
        <Button
          type="submit"
          variant="outline"
          touch
          className="mt-2 justify-self-start"
          disabled={busy}
          aria-busy={busy}
        >
          Change password and sign out
        </Button>
      </form>
    </section>
  );
}
