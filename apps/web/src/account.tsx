import { api, errorDetail, readBootstrap } from "@diffx/api";
import { ArrowLeftIcon, UserRoundIcon } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { ErrorState } from "@/components/error-state";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { navigate, useIdentity } from "./identity-context.tsx";
import { AccountGitHub, type GitHubFollowing } from "./account-github.tsx";
import { AccountNotifications } from "./account-notifications.tsx";
import { AccountTokens } from "./account-tokens.tsx";
import { forgetPushDevice } from "./push.ts";

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

const prose = "mb-4 leading-copy text-secondary";

/** Failed outcomes the Google callback reports through `/?signin=`. */
const signInFailures: Record<string, string> = {
  "no-account":
    "No diffx account uses that Google account. Ask us for an invite.",
  "invite-mismatch":
    "That Google account does not match the invited email, or Google has not verified it. Open the invite link again and choose the invited account.",
  "invite-invalid":
    "The invite link is invalid, expired or already used. Ask us for a new one.",
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

/** Members sign in with Google; accounts come only from invites. */
export function SignIn() {
  const { state } = useIdentity();
  const [outcome] = useState(takeSignInOutcome);
  const google =
    state.status === "ready" &&
    state.bootstrap.mode === "remote" &&
    state.bootstrap.googleEnabled;
  const failure = signInFailures[outcome];
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Sign in</h1>
      {outcome === "cancelled" && (
        <p className={prose} role="status">
          Google sign-in was cancelled.
        </p>
      )}
      {failure && <ErrorState message={failure} className="mb-4" />}
      {google ? (
        <>
          <p className={prose}>
            Use the Google account your invite was sent to.
          </p>
          <ContinueWithGoogle />
        </>
      ) : (
        <p className={prose}>
          Sign-in is not available on this server yet. Ask us for an invite.
        </p>
      )}
    </main>
  );
}

/** Starts Google sign-in, accepting the invite when one is given. */
function ContinueWithGoogle({ invite }: { invite?: string }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function start() {
    setBusy(true);
    setError("");
    try {
      const { data, error } =
        invite === undefined
          ? await api.POST("/api/v2/auth/google", { body: {} })
          : await api.POST("/api/v2/invites/google", {
              body: { token: invite },
            });
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
        className="self-start"
        disabled={busy}
        aria-busy={busy}
        onClick={() => void start()}
      >
        Continue with Google
      </Button>
    </>
  );
}

function fragmentToken() {
  return new URLSearchParams(window.location.hash.slice(1)).get("t") ?? "";
}

type InviteState =
  | { status: "loading" }
  | { status: "invalid" }
  | { status: "error"; message: string }
  | { status: "ready"; email: string; expiresAt: number };

/** Accepts an invite link: its secret stays in the fragment, never a request URL. */
export function InviteSignUp() {
  const [token, setToken] = useState(fragmentToken);
  const [invite, setInvite] = useState<InviteState>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    // Another link opened in this tab changes only the fragment.
    const changed = () => {
      const next = fragmentToken();
      window.history.replaceState(null, "", "/invite");
      if (next) setToken(next);
    };
    window.addEventListener("hashchange", changed);
    return () => window.removeEventListener("hashchange", changed);
  }, []);
  useEffect(() => {
    // The secret stays out of history once read.
    window.history.replaceState(null, "", "/invite");
    if (!token) {
      setInvite({ status: "invalid" });
      return;
    }
    const controller = new AbortController();
    setInvite({ status: "loading" });
    void api
      .POST("/api/v2/invites/lookup", {
        body: { token },
        signal: controller.signal,
      })
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setInvite({ status: "invalid" });
          return;
        }
        if (!data)
          throw new Error(errorDetail(error, "Unable to load the invite"));
        setInvite({
          status: "ready",
          email: data.email,
          expiresAt: data.expiresAt,
        });
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setInvite({
            status: "error",
            message: errorDetail(failure, "Unable to load the invite"),
          });
      });
    return () => controller.abort();
  }, [token, attempt]);
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Join diffx</h1>
      {invite.status === "loading" && (
        <p className={prose} role="status">
          Checking your invite…
        </p>
      )}
      {invite.status === "error" && (
        <ErrorState
          message={invite.message}
          className="mb-4"
          touch
          onRetry={() => setAttempt((value) => value + 1)}
        />
      )}
      {invite.status === "invalid" && (
        <p className={prose}>
          This invite link is invalid, expired or already used. Ask us for a new
          one.
        </p>
      )}
      {invite.status === "ready" && (
        <>
          <p className={prose}>
            You are invited as <strong>{invite.email}</strong>. Continue with
            the Google account for that email. The invite expires{" "}
            {new Date(invite.expiresAt).toLocaleString()}.
          </p>
          <ContinueWithGoogle invite={token} />
        </>
      )}
    </main>
  );
}

/** Password sign-in for the admin only, at the unlinked /console. */
export function AdminSignIn() {
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Sign in</h1>
      <p className="mb-4 leading-copy text-secondary">
        Sign in with your username and password.
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

/**
 * One-time admin creation from the setup link the server logged at startup.
 * Once setup is complete the page is the sign-in page, like any unknown path.
 */
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
  if (setup === "complete") return <SignIn />;
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-2 text-xl font-semibold">Set up diffx</h1>
      {error && <ErrorState message={error} />}
      {setup === "required" && !token && (
        <p className="mb-4 leading-copy text-secondary">
          Open the setup link from the server log to create the first account.
        </p>
      )}
      {setup === "required" && token && (
        <>
          <p className="mb-4 leading-copy text-secondary">
            Create the server's first account. It signs in with this password
            and invites everyone else.
          </p>
          <CredentialForm
            path="/api/v2/setup"
            token={token}
            submitLabel="Create account"
            failure="Unable to create the account. Try again."
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

/** A signed-in account page: the way back to reviews, then its content. */
export function AccountPage({
  username,
  children,
}: {
  username: string;
  children: ReactNode;
}) {
  return (
    <div className="account-shell">
      <header className="topbar account-topbar">
        <Button variant="shell" onClick={() => navigate("/")}>
          <ArrowLeftIcon aria-hidden="true" /> Reviews
        </Button>
        <span className="header-actions">{username}</span>
      </header>
      <main className="account-page settings-page">{children}</main>
    </div>
  );
}

export function AccountSettings() {
  const { state, signedOut } = useIdentity();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [github, setGitHub] = useState<GitHubFollowing | null>(null);
  const [pushEnabled, setPushEnabled] = useState(false);
  if (state.status !== "ready" || !state.bootstrap.user) return null;
  const user = state.bootstrap.user;
  async function logout() {
    setBusy(true);
    setError("");
    try {
      // This browser stops receiving the account's notifications first.
      await forgetPushDevice();
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
    <AccountPage username={user.username}>
      <h1 className="mb-2 text-xl font-semibold">Account settings</h1>
      <p className={prose}>
        Signed in as <strong>{user.username}</strong>
        {user.email !== null && <> with Google as {user.email}</>}.
      </p>
      {user.admin && (
        <Button
          variant="outline"
          touch
          className="mb-4"
          onClick={() => navigate("/manage")}
        >
          Manage accounts and invites
        </Button>
      )}
      <AccountTokens />
      <AccountGitHub notify={pushEnabled} onFollowingChange={setGitHub} />
      <AccountNotifications
        admin={user.admin}
        github={github}
        onEnabledChange={setPushEnabled}
      />
      {user.admin && <PasswordSettings />}
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
    </AccountPage>
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
