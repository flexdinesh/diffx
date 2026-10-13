import { api, errorDetail, readBootstrap } from "@diffx/api";
import { ArrowLeftIcon, UserRoundIcon } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { navigate, useIdentity } from "./identity-context.tsx";
import { AccountTokens } from "./account-tokens.tsx";

export function AccountButton() {
  const { state } = useIdentity();
  if (
    state.status !== "ready" ||
    state.bootstrap.mode !== "remote" ||
    !state.bootstrap.user
  )
    return null;
  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label="Account settings"
      title={`Account settings · ${state.bootstrap.user.username}`}
      onClick={() => navigate("/settings")}
    >
      <UserRoundIcon className="size-(--icon-base)" aria-hidden="true" />
    </Button>
  );
}

export function SignIn() {
  const { state, establish } = useIdentity();
  const [signup, setSignup] = useState(false);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const signupEnabled =
    state.status === "ready" && state.bootstrap.signupEnabled;
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { data, error } = await api.POST(
        signup ? "/api/v2/auth/signup" : "/api/v2/auth/login",
        { body: { username, password } },
      );
      if (!data)
        throw new Error(errorDetail(error, "Unable to sign in. Try again."));
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
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to sign in. Try again."));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1>{signup ? "Create your account" : "Sign in"}</h1>
      <p>Review your collected changes on this server.</p>
      <form className="account-form" onSubmit={submit}>
        <label htmlFor="username">Username</label>
        <Input
          id="username"
          name="username"
          autoComplete="username"
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
          autoComplete={signup ? "new-password" : "current-password"}
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
          minLength={signup ? 12 : undefined}
          maxLength={128}
          disabled={busy}
        />
        {signup && <p className="account-hint">Use at least 12 characters.</p>}
        {error && (
          <p className="account-error" role="alert">
            {error}
          </p>
        )}
        <Button type="submit" disabled={busy} aria-busy={busy}>
          {signup ? "Create account" : "Sign in"}
        </Button>
      </form>
      {signupEnabled && (
        <Button
          variant="link"
          disabled={busy}
          onClick={() => {
            setSignup((value) => !value);
            setError("");
            setPassword("");
          }}
        >
          {signup ? "Already have an account? Sign in" : "Create an account"}
        </Button>
      )}
    </main>
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
        <Button variant="ghost" onClick={() => navigate("/")}>
          <ArrowLeftIcon aria-hidden="true" /> Reviews
        </Button>
        <span className="header-actions">{state.bootstrap.user.username}</span>
      </header>
      <main className="account-page settings-page">
        <h1>Account settings</h1>
        <p>
          Signed in as <strong>{state.bootstrap.user.username}</strong>.
        </p>
        <AccountTokens />
        <PasswordSettings />
        <section className="account-section" aria-labelledby="session-title">
          <h2 id="session-title">Browser session</h2>
          {error && (
            <p className="account-error" role="alert">
              {error}
            </p>
          )}
          <Button
            variant="outline"
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
    <section className="account-section" aria-labelledby="password-title">
      <h2 id="password-title">Password</h2>
      <p>
        Changing your password signs out every browser session. API tokens
        remain active.
      </p>
      <form className="account-form" onSubmit={submit}>
        <label htmlFor="current-password">Current password</label>
        <Input
          id="current-password"
          type="password"
          autoComplete="current-password"
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
          minLength={12}
          maxLength={128}
          value={newPassword}
          onChange={(event) => setNew(event.target.value)}
          required
          disabled={busy}
        />
        <p className="account-hint">Use at least 12 characters.</p>
        {error && (
          <p className="account-error" role="alert">
            {error}
          </p>
        )}
        <Button
          type="submit"
          variant="outline"
          disabled={busy}
          aria-busy={busy}
        >
          Change password and sign out
        </Button>
      </form>
    </section>
  );
}
