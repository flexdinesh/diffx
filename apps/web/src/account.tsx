import { api, errorDetail, readBootstrap } from "@diffx/api";
import { ArrowLeftIcon, UserRoundIcon } from "lucide-react";
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import { Separator } from "@diffx/ui/components/ui/separator";
import { navigate, useIdentity } from "./identity-context.tsx";
import { AccountGitHub, type GitHubFollowing } from "./account-github.tsx";
import { AccountNotifications } from "./account-notifications.tsx";
import { AccountTokens } from "./account-tokens.tsx";
import { forgetPushDevice } from "./push.ts";
import {
  returnQuery,
  signInReturn,
  type ReturnPage,
} from "./public-pull-request.ts";
import {
  SettingsLayout,
  SettingsSection,
  SettingsSectionAbsent,
  type SettingsSectionId,
} from "./settings-layout.tsx";

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
      hint={`Account settings · ${state.bootstrap.user.email}`}
      onClick={() => navigate("/settings")}
    >
      <UserRoundIcon aria-hidden="true" />
    </IconButton>
  );
}

export const prose = "mb-4 leading-copy text-secondary";

/** Failed outcomes the Google callback reports through `/?signin=`. */
const signInFailures: Record<string, string> = {
  "no-account":
    "No diffx account uses that Google account. Ask us for an invite.",
  "invite-mismatch":
    "That Google account does not match the invited email, or Google has not verified it. Open the invite link again and choose the invited account.",
  "invite-invalid":
    "The invite link is invalid, expired or already used. Ask us for a new one.",
  "email-taken":
    "A password account already uses that email. Sign in with your password instead.",
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

/**
 * Whether open signup offers a method: Google, or a password while new
 * accounts may choose one.
 */
export function signupOpen(remote: {
  googleEnabled: boolean;
  signupEnabled: boolean;
  passwordSignupEnabled: boolean;
}) {
  return (
    remote.signupEnabled &&
    (remote.googleEnabled || remote.passwordSignupEnabled)
  );
}

/**
 * The deployment's methods, from the bootstrap. Existing password accounts
 * always sign in; `password` is whether new accounts may choose one.
 */
function useMethods() {
  const { state } = useIdentity();
  const remote =
    state.status === "ready" && state.bootstrap.mode === "remote"
      ? state.bootstrap
      : null;
  return {
    google: remote?.googleEnabled === true,
    password: remote?.passwordSignupEnabled === true,
    signup: remote !== null && signupOpen(remote),
  };
}

/** An in-app link that keeps the page loaded. */
export function PageLink({
  to,
  children,
}: {
  to: string;
  children: ReactNode;
}) {
  return (
    <a
      href={to}
      className="text-accent underline underline-offset-2"
      onClick={(event) => {
        event.preventDefault();
        navigate(to);
      }}
    >
      {children}
    </a>
  );
}

/** The signed-out page shell shared by every account form. */
export function AuthPage({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <main className="account-page auth-page">
      <a className="brand" href="/">
        diffx
      </a>
      <h1 className="mb-4 text-xl font-semibold">{title}</h1>
      {children}
    </main>
  );
}

/** Separates Google from the password form when both are offered. */
function MethodSeparator() {
  return <Separator className="my-6" />;
}

/**
 * Signs in with Google first. The password form shows beside it while new
 * accounts may choose a password, and otherwise behind a quiet link for
 * existing password accounts; without Google it is the only way in.
 */
export function SignIn() {
  const { google, password, signup } = useMethods();
  const [outcome] = useState(takeSignInOutcome);
  const [next] = useState(() => signInReturn(window.location.search));
  const [revealed, setRevealed] = useState(false);
  const failure = signInFailures[outcome];
  const form = !google || password || revealed;
  return (
    <AuthPage title="Sign in">
      {outcome === "cancelled" && (
        <p className={prose} role="status">
          Google sign-in was cancelled.
        </p>
      )}
      {failure && <ErrorState message={failure} className="mb-4" />}
      {google && <ContinueWithGoogle secondary={form} next={next} />}
      {google && form && <MethodSeparator />}
      {form ? (
        <CredentialForm
          path="/api/v2/auth/login"
          submitLabel="Sign in"
          failure="Unable to sign in. Check your email and password."
          autoFocus={revealed}
          next={next}
        />
      ) : (
        <p className="mt-4 text-sm leading-copy text-secondary">
          <Button
            variant="link"
            size="inline"
            touch
            onClick={() => setRevealed(true)}
          >
            Sign in with password
          </Button>
        </p>
      )}
      <p className="mt-6 leading-copy text-secondary">
        {signup ? (
          <>
            New here?{" "}
            <PageLink to={`/signup${returnQuery(next)}`}>
              Create an account
            </PageLink>
          </>
        ) : (
          "Accounts are by invite. Ask us for one."
        )}
      </p>
    </AuthPage>
  );
}

/** Open signup with each enabled method; Google supplies the account email. */
export function SignUp() {
  const { google, password } = useMethods();
  const [next] = useState(() => signInReturn(window.location.search));
  return (
    <AuthPage title="Create an account">
      {google && (
        <>
          <p className={prose}>
            Continue with Google to sign up with your verified email.
          </p>
          <ContinueWithGoogle secondary={password} next={next} />
        </>
      )}
      {google && password && <MethodSeparator />}
      {password && (
        <CredentialForm
          path="/api/v2/auth/signup"
          submitLabel="Create account"
          failure="Unable to create the account. Try again."
          next={next}
        />
      )}
      <p className="mt-6 leading-copy text-secondary">
        Already have an account?{" "}
        <PageLink to={`/signin${returnQuery(next)}`}>Sign in</PageLink>
      </p>
    </AuthPage>
  );
}

/**
 * Starts Google sign-in, accepting the invite when one is given. Secondary
 * beside a password form, so each page keeps one primary action.
 */
function ContinueWithGoogle({
  invite,
  secondary = false,
  next = null,
}: {
  invite?: string;
  secondary?: boolean;
  /** The page the callback opens instead of `/`. */
  next?: ReturnPage | null;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function start() {
    setBusy(true);
    setError("");
    try {
      const { data, error } =
        invite === undefined
          ? await api.POST("/api/v2/auth/google", {
              body: next ? { next } : {},
            })
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
        variant={secondary ? "outline" : "default"}
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

/**
 * Reads a link secret from the fragment once and drops it from history, so it
 * never reaches a request URL or the back button. Another link opened in this
 * tab changes only the fragment.
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

type LinkState<T> =
  | { status: "loading" }
  | { status: "invalid" }
  | { status: "error"; message: string }
  | { status: "ready"; value: T };

const lookupInvite = (token: string, signal: AbortSignal) =>
  api.POST("/api/v2/invites/lookup", { body: { token }, signal });
const lookupReset = (token: string, signal: AbortSignal) =>
  api.POST("/api/v2/password-reset/lookup", { body: { token }, signal });

/**
 * Resolves a link secret; 404 means invalid, expired or used. Pass a stable
 * lookup, such as one defined at module scope.
 */
function useLink<T>(
  token: string,
  lookup: (
    token: string,
    signal: AbortSignal,
  ) => Promise<{ data?: T; error?: unknown; response: Response }>,
  failure: string,
) {
  const [link, setLink] = useState<LinkState<T>>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!token) {
      setLink({ status: "invalid" });
      return;
    }
    const controller = new AbortController();
    setLink({ status: "loading" });
    void lookup(token, controller.signal)
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setLink({ status: "invalid" });
          return;
        }
        if (!data) throw new Error(errorDetail(error, failure));
        setLink({ status: "ready", value: data });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setLink({ status: "error", message: errorDetail(error, failure) });
      });
    return () => controller.abort();
  }, [token, attempt, lookup, failure]);
  return { link, retry: () => setAttempt((value) => value + 1) };
}

/** Accepts an invite link with Google or, where allowed, a password. */
export function InviteSignUp() {
  const { google, password } = useMethods();
  const token = useFragmentToken("/invite");
  const { link, retry } = useLink(
    token,
    lookupInvite,
    "Unable to load the invite",
  );
  return (
    <AuthPage title="Join diffx">
      {link.status === "loading" && (
        <p className={prose} role="status">
          Checking your invite…
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
          This invite link is invalid, expired or already used. Ask us for a new
          one.
        </p>
      )}
      {link.status === "ready" && (
        <>
          <p className={prose}>
            You are invited as <strong>{link.value.email}</strong>. The invite
            expires {new Date(link.value.expiresAt).toLocaleString()}.
          </p>
          {google && (
            <>
              <p className={prose}>
                Continue with the Google account for that email.
              </p>
              <ContinueWithGoogle invite={token} secondary={password} />
            </>
          )}
          {google && password && <MethodSeparator />}
          {password && (
            <>
              <p className={prose}>
                {google ? "Or choose" : "Choose"} a password for this email.
              </p>
              <CredentialForm
                path="/api/v2/invites/password"
                token={token}
                invitedEmail={link.value.email}
                submitLabel="Create account"
                failure="Unable to create the account. Try again."
              />
            </>
          )}
        </>
      )}
    </AuthPage>
  );
}

/** Sets a new password from a reset link an admin issued. */
export function ResetPassword() {
  const token = useFragmentToken("/reset-password");
  const { link, retry } = useLink(
    token,
    lookupReset,
    "Unable to load the reset link",
  );
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
      const { response, error } = await api.POST("/api/v2/password-reset", {
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
          <PageLink to="/signin">Sign in</PageLink> with the new password.
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
              This reset link is invalid, expired or already used. Ask us for a
              new one.
            </p>
          )}
          {link.status === "ready" && (
            <>
              <p className={prose}>
                Choose a new password for <strong>{link.value.email}</strong>.
                This signs out your browser sessions; API tokens remain active.
              </p>
              <form className="account-form" onSubmit={submit}>
                {/* Lets password managers save the new password for this account. */}
                <Input
                  type="text"
                  name="email"
                  autoComplete="username"
                  value={link.value.email}
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

/** Email and password sign-in or account creation. */
function CredentialForm({
  path,
  token = "",
  invitedEmail,
  submitLabel,
  failure,
  autoFocus = false,
  next = null,
}: {
  path:
    | "/api/v2/auth/login"
    | "/api/v2/auth/signup"
    | "/api/v2/invites/password";
  token?: string;
  invitedEmail?: string;
  submitLabel: string;
  failure: string;
  /** Focuses the email field when the form appears on request. */
  autoFocus?: boolean;
  /** The page to open once signed in; otherwise the app's home. */
  next?: ReturnPage | null;
}) {
  const { establish } = useIdentity();
  const [email, setEmail] = useState(invitedEmail ?? "");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const creating = path !== "/api/v2/auth/login";
  async function submit(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    try {
      const { data, error } =
        path === "/api/v2/invites/password"
          ? await api.POST(path, { body: { token, email, password } })
          : await api.POST(path, { body: { email, password } });
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
      if (next) navigate(next, { replace: true });
    } catch (failed: unknown) {
      setError(errorDetail(failed, failure));
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="account-form" onSubmit={submit}>
      <label htmlFor="email">Email</label>
      <Input
        id="email"
        name="email"
        type="email"
        autoComplete="username"
        touch
        value={email}
        onChange={(event) => setEmail(event.target.value)}
        autoFocus={autoFocus}
        required
        minLength={3}
        maxLength={254}
        readOnly={invitedEmail !== undefined}
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

/** A signed-in account page: the way back to reviews, then its content. */
export function AccountPage({
  email,
  children,
}: {
  email: string;
  children: ReactNode;
}) {
  return (
    <div className="account-shell">
      <header className="topbar account-topbar">
        <Button variant="shell" onClick={() => navigate("/")}>
          <ArrowLeftIcon aria-hidden="true" /> Reviews
        </Button>
        <span className="header-actions account-identity">{email}</span>
      </header>
      {children}
    </div>
  );
}

export function AccountSettings({
  section,
}: {
  /** The page the route names; null opens the first section. */
  section: SettingsSectionId | null;
}) {
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
    <AccountPage email={user.email}>
      <SettingsLayout
        section={section}
        header={
          <header className="settings-summary">
            <div className="min-w-0 wrap-anywhere">
              <h1 className="mb-1 text-xl font-semibold">Account settings</h1>
              <p className="leading-copy text-secondary">
                Signed in as <strong>{user.email}</strong>
                {user.method === "google" && " with Google"}.
              </p>
            </div>
            <Button
              variant="outline"
              touch
              disabled={busy}
              aria-busy={busy}
              onClick={() => void logout()}
            >
              Sign out
            </Button>
            {error && <ErrorState message={error} className="basis-full" />}
          </header>
        }
      >
        <AccountTokens />
        <AccountGitHub notify={pushEnabled} onFollowingChange={setGitHub} />
        <AccountNotifications
          github={github}
          onEnabledChange={setPushEnabled}
        />
        {user.method === "password" ? (
          <PasswordSettings />
        ) : (
          <SettingsSectionAbsent id="password" />
        )}
      </SettingsLayout>
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
    <SettingsSection
      id="password"
      description="Changing it signs out every browser session. API tokens stay active."
    >
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
          minLength={8}
          maxLength={128}
          value={newPassword}
          onChange={(event) => setNew(event.target.value)}
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
          variant="outline"
          touch
          className="mt-2 justify-self-start"
          disabled={busy}
          aria-busy={busy}
        >
          Change password and sign out
        </Button>
      </form>
    </SettingsSection>
  );
}
