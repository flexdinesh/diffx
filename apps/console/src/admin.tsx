import { api, errorDetail } from "@diffx/console-api";
import { useState, type FormEvent } from "react";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import { Page, prose } from "./page.tsx";
import { useAdmin, useSession } from "./session.tsx";

const section = "mt-8 border-t border-separator pt-6";

/** The signed-in admin's own account: password and this browser's session. */
export function Admin() {
  const admin = useAdmin();
  const { signedOut } = useSession();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function logout() {
    setBusy(true);
    setError("");
    try {
      const { response, error } = await api.POST("/api/v1/auth/logout");
      if (!response.ok)
        throw new Error(errorDetail(error, "Unable to sign out. Try again."));
      signedOut();
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to sign out. Try again."));
      setBusy(false);
    }
  }
  return (
    <Page
      title="Admin"
      description={
        <>
          Signed in as <strong>{admin.username}</strong>
          {admin.email !== null && <> with Google as {admin.email}</>}. Other
          admins manage your role and access; your app settings live in the
          diffx app.
        </>
      }
    >
      {admin.method === "password" && <PasswordSettings />}
      <section className={section} aria-labelledby="session-title">
        <h2 id="session-title" className="mb-2 text-md font-semibold">
          Browser session
        </h2>
        <p className={prose}>
          Signing out ends this browser&apos;s console session only.
        </p>
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
    </Page>
  );
}

function PasswordSettings() {
  const { signedOut } = useSession();
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
      const { response, error } = await api.PUT("/api/v1/account/password", {
        body: { currentPassword, newPassword },
      });
      if (!response.ok)
        throw new Error(
          errorDetail(error, "Unable to change password. Try again."),
        );
      signedOut();
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to change password. Try again."));
      setBusy(false);
    }
  }
  return (
    <section className={section} aria-labelledby="password-title">
      <h2 id="password-title" className="mb-2 text-md font-semibold">
        Password
      </h2>
      <p className={prose}>
        Your password signs in to this console and the diffx app. Changing it
        signs out every browser session; API tokens remain active.
      </p>
      <form className="console-form" onSubmit={submit}>
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
    </section>
  );
}
