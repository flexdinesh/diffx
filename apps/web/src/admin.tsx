import {
  api,
  errorDetail,
  type ApiAdminUser as AdminUser,
  type ApiInvite as Invite,
} from "@diffx/api";
import { useEffect, useState, type FormEvent } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { AccountPage } from "./account.tsx";
import { useIdentity } from "./identity-context.tsx";

const prose = "mb-4 leading-copy text-secondary";
const row =
  "flex flex-wrap items-center justify-between gap-4 border-b border-separator py-3";

function date(value: number) {
  return new Date(value).toLocaleDateString();
}

/** Loads a list and reloads it on demand, keeping the last list meanwhile. */
function useAdminList<T>(load: (signal: AbortSignal) => Promise<T[]>) {
  const [items, setItems] = useState<T[] | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    load(controller.signal)
      .then((value) => {
        if (!controller.signal.aborted) setItems(value);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(errorDetail(failure, "Unable to load. Try again."));
      });
    return () => controller.abort();
  }, [load, attempt]);
  return { items, error, reload: () => setAttempt((value) => value + 1) };
}

async function loadUsers(signal: AbortSignal) {
  const { data, error } = await api.GET("/api/v2/admin/users", { signal });
  if (!data)
    throw new Error(errorDetail(error, "Unable to load accounts. Try again."));
  return data.users;
}

async function loadInvites(signal: AbortSignal) {
  const { data, error } = await api.GET("/api/v2/admin/invites", { signal });
  if (!data)
    throw new Error(errorDetail(error, "Unable to load invites. Try again."));
  return data.invites;
}

/** The admin's page: invite people and manage every account. */
export function AdminPage() {
  const { state } = useIdentity();
  if (
    state.status !== "ready" ||
    state.bootstrap.mode !== "remote" ||
    !state.bootstrap.user
  )
    return null;
  return (
    <AccountPage username={state.bootstrap.user.username}>
      <h1 className="mb-2 text-xl font-semibold">Admin</h1>
      <p className={prose}>Invite people and manage accounts on this server.</p>
      <AdminInvites google={state.bootstrap.googleEnabled} />
      <AdminAccounts />
    </AccountPage>
  );
}

const inviteStatus: Record<
  Invite["status"],
  { label: string; tone: "accent" | "success" | "neutral" }
> = {
  pending: { label: "Pending", tone: "accent" },
  used: { label: "Used", tone: "success" },
  expired: { label: "Expired", tone: "neutral" },
  revoked: { label: "Revoked", tone: "neutral" },
};

function inviteDetail(invite: Invite) {
  switch (invite.status) {
    case "pending":
      return `Expires ${new Date(invite.expiresAt).toLocaleString()}`;
    case "used":
      return invite.usedBy === null
        ? "Account deleted"
        : `Joined as ${invite.usedBy}`;
    case "expired":
      return `Expired ${date(invite.expiresAt)}`;
    case "revoked":
      return `Created ${date(invite.createdAt)}`;
  }
}

function AdminInvites({ google }: { google: boolean }) {
  const invites = useAdminList(loadInvites);
  const [email, setEmail] = useState("");
  const [created, setCreated] = useState<{
    invite: Invite;
    url: string;
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copyStatus, setCopyStatus] = useState("");
  const [revoking, setRevoking] = useState<Invite | null>(null);
  const [revokeError, setRevokeError] = useState("");
  async function create(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setCreated(null);
    setCopyStatus("");
    try {
      const { data, error } = await api.POST("/api/v2/admin/invites", {
        body: { email },
      });
      if (!data)
        throw new Error(
          errorDetail(error, "Unable to create invite. Try again."),
        );
      setCreated(data);
      setEmail("");
      invites.reload();
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to create invite. Try again."));
    } finally {
      setBusy(false);
    }
  }
  async function revoke(id: string) {
    setBusy(true);
    setRevokeError("");
    try {
      const { response, error } = await api.DELETE(
        "/api/v2/admin/invites/{inviteId}",
        { params: { path: { inviteId: id } } },
      );
      if (!response.ok)
        throw new Error(
          errorDetail(error, "Unable to revoke invite. Try again."),
        );
      if (created?.invite.id === id) setCreated(null);
      setRevoking(null);
      invites.reload();
    } catch (failure: unknown) {
      setRevokeError(
        errorDetail(failure, "Unable to revoke invite. Try again."),
      );
    } finally {
      setBusy(false);
    }
  }
  async function copy() {
    try {
      await navigator.clipboard.writeText(created?.url ?? "");
      setCopyStatus("Copied");
    } catch {
      setCopyStatus("Copy unavailable. Select and copy the link below.");
    }
  }
  return (
    <section
      className="mt-8 border-t border-separator pt-6"
      aria-labelledby="invites-title"
    >
      <h2 id="invites-title" className="mb-2 text-md font-semibold">
        Invites
      </h2>
      {google ? (
        <p className={prose}>
          An invite lets one person join with the Google account for that email.
          Links work once and expire after 48 hours; inviting an email again
          replaces its pending link.
        </p>
      ) : (
        <p className={prose}>
          Invites need Google sign-in. Set DIFFX_SERVER_GOOGLE_CLIENT_ID,
          DIFFX_SERVER_GOOGLE_CLIENT_SECRET and DIFFX_SERVER_PUBLIC_URL, then
          restart the server.
        </p>
      )}
      {invites.error && (
        <ErrorState
          message={invites.error}
          className="mb-4"
          touch
          onRetry={invites.reload}
        />
      )}
      {invites.items === null && !invites.error && (
        <p className={prose} role="status">
          Loading invites…
        </p>
      )}
      {invites.items !== null && invites.items.length > 0 && (
        <ul className="mb-6">
          {invites.items.map((invite) => (
            <li key={invite.id} className={row}>
              <div className="min-w-0 wrap-anywhere">
                <strong className="block font-medium">{invite.email}</strong>
                <span className="mt-1 flex items-center gap-2 text-sm text-muted">
                  <Badge
                    variant="status"
                    tone={inviteStatus[invite.status].tone}
                  >
                    {inviteStatus[invite.status].label}
                  </Badge>
                  {inviteDetail(invite)}
                </span>
              </div>
              {invite.status === "pending" && (
                <Button
                  variant="destructive"
                  touch
                  disabled={busy}
                  aria-label={`Revoke invite for ${invite.email}`}
                  onClick={() => {
                    setRevokeError("");
                    setRevoking(invite);
                  }}
                >
                  Revoke
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={revoking !== null}
        onOpenChange={(open) => {
          if (!open) setRevoking(null);
        }}
        title={`Revoke invite for ${revoking?.email ?? "this email"}?`}
        description="The invite link stops working immediately. You can invite this email again."
        error={revokeError}
        confirmLabel="Revoke invite"
        busyLabel="Revoking…"
        busy={busy}
        onConfirm={() => {
          if (revoking) void revoke(revoking.id);
        }}
      />
      {google && (
        <form className="account-form" onSubmit={create}>
          <label htmlFor="invite-email">Email</label>
          <Input
            id="invite-email"
            type="email"
            autoComplete="off"
            required
            maxLength={254}
            placeholder="name@example.com"
            touch
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            disabled={busy}
          />
          {error && <ErrorState message={error} />}
          <Button
            type="submit"
            touch
            className="mt-2 justify-self-start"
            disabled={busy}
            aria-busy={busy}
          >
            Create invite
          </Button>
        </form>
      )}
      {created && (
        <div
          className="mt-6 rounded-control border border-separator bg-panel p-4"
          role="status"
        >
          <h3 className="mb-2 text-md font-semibold">Send this invite link</h3>
          <p className={prose}>
            It works once, for {created.invite.email}, until{" "}
            {new Date(created.invite.expiresAt).toLocaleString()}. The link will
            disappear when you leave this page.
          </p>
          <pre
            tabIndex={0}
            className="my-4 font-mono text-sm whitespace-pre-wrap wrap-anywhere select-all"
          >
            {created.url}
          </pre>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" touch onClick={() => void copy()}>
              Copy link
            </Button>
            <Button variant="ghost" touch onClick={() => setCreated(null)}>
              Dismiss link
            </Button>
          </div>
          {copyStatus && <p className={prose}>{copyStatus}</p>}
        </div>
      )}
    </section>
  );
}

type AccountAction = { kind: "disable" | "delete"; user: AdminUser };

function AdminAccounts() {
  const users = useAdminList(loadUsers);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [confirming, setConfirming] = useState<AccountAction | null>(null);
  const [confirmError, setConfirmError] = useState("");
  async function apply(kind: "disable" | "enable" | "delete", user: AdminUser) {
    setBusy(true);
    setError("");
    setConfirmError("");
    const failure = `Unable to ${kind} ${user.username}. Try again.`;
    try {
      const params = { params: { path: { userId: user.id } } };
      const { response, error } =
        kind === "delete"
          ? await api.DELETE("/api/v2/admin/users/{userId}", params)
          : kind === "disable"
            ? await api.POST("/api/v2/admin/users/{userId}/disable", params)
            : await api.POST("/api/v2/admin/users/{userId}/enable", params);
      if (!response.ok) throw new Error(errorDetail(error, failure));
      setConfirming(null);
      users.reload();
    } catch (failed: unknown) {
      if (kind === "enable") setError(errorDetail(failed, failure));
      else setConfirmError(errorDetail(failed, failure));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="mt-8 border-t border-separator pt-6"
      aria-labelledby="accounts-title"
    >
      <h2 id="accounts-title" className="mb-2 text-md font-semibold">
        Accounts
      </h2>
      <p className={prose}>
        Disabling signs an account out everywhere and revokes its API tokens.
        Deleting also removes its reviews.
      </p>
      {users.error && (
        <ErrorState
          message={users.error}
          className="mb-4"
          touch
          onRetry={users.reload}
        />
      )}
      {error && <ErrorState message={error} className="mb-4" />}
      {users.items === null && !users.error && (
        <p className={prose} role="status">
          Loading accounts…
        </p>
      )}
      {users.items !== null && (
        <ul>
          {users.items.map((user) => (
            <li key={user.id} className={row}>
              <div className="min-w-0 wrap-anywhere">
                <strong className="flex items-center gap-2 font-medium">
                  {user.username}
                  {user.admin && <Badge variant="status">Admin</Badge>}
                  {user.disabled && (
                    <Badge variant="status" tone="warning">
                      Disabled
                    </Badge>
                  )}
                </strong>
                <span className="mt-1 block text-sm text-muted">
                  {user.email ?? "Password sign-in"} · Joined{" "}
                  {date(user.createdAt)}
                </span>
              </div>
              {!user.admin && (
                <div className="flex flex-wrap gap-2">
                  {user.disabled ? (
                    <Button
                      variant="outline"
                      touch
                      disabled={busy}
                      aria-label={`Enable ${user.username}`}
                      onClick={() => void apply("enable", user)}
                    >
                      Enable
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      touch
                      disabled={busy}
                      aria-label={`Disable ${user.username}`}
                      onClick={() => {
                        setConfirmError("");
                        setConfirming({ kind: "disable", user });
                      }}
                    >
                      Disable
                    </Button>
                  )}
                  <Button
                    variant="destructive"
                    touch
                    disabled={busy}
                    aria-label={`Delete ${user.username}`}
                    onClick={() => {
                      setConfirmError("");
                      setConfirming({ kind: "delete", user });
                    }}
                  >
                    Delete
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(open) => {
          if (!open) setConfirming(null);
        }}
        title={
          confirming?.kind === "delete"
            ? `Delete ${confirming.user.username}?`
            : `Disable ${confirming?.user.username ?? "account"}?`
        }
        description={
          confirming?.kind === "delete"
            ? "The account, its reviews, comments and API tokens are removed. This cannot be undone."
            : "The account is signed out everywhere and its API tokens are revoked. Enabling it later restores sign-in, not those tokens."
        }
        error={confirmError}
        confirmLabel={
          confirming?.kind === "delete" ? "Delete account" : "Disable account"
        }
        busyLabel={confirming?.kind === "delete" ? "Deleting…" : "Disabling…"}
        busy={busy}
        onConfirm={() => {
          if (confirming) void apply(confirming.kind, confirming.user);
        }}
      />
    </section>
  );
}
