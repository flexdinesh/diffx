import { api, errorDetail, type ConsoleInvite } from "@diffx/console-api";
import { useState, type FormEvent } from "react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import { OneTimeLink, Page, date, prose, row, useList } from "./page.tsx";

async function loadInvites(signal: AbortSignal) {
  const { data, error } = await api.GET("/api/v1/invites", { signal });
  if (!data)
    throw new Error(errorDetail(error, "Unable to load invites. Try again."));
  return data.invites;
}

const inviteStatus: Record<
  ConsoleInvite["status"],
  { label: string; tone: "accent" | "success" | "neutral" }
> = {
  pending: { label: "Pending", tone: "accent" },
  used: { label: "Used", tone: "success" },
  expired: { label: "Expired", tone: "neutral" },
  revoked: { label: "Revoked", tone: "neutral" },
};

function inviteDetail(invite: ConsoleInvite) {
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

/** Invite people to the diffx app as members. */
export function Invites() {
  const invites = useList(loadInvites, "Unable to load invites. Try again.");
  const [email, setEmail] = useState("");
  const [created, setCreated] = useState<{
    invite: ConsoleInvite;
    url: string;
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [revoking, setRevoking] = useState<ConsoleInvite | null>(null);
  const [revokeError, setRevokeError] = useState("");
  async function create(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setCreated(null);
    try {
      const { data, error } = await api.POST("/api/v1/invites", {
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
        "/api/v1/invites/{inviteId}",
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
  return (
    <Page
      title="Invites"
      description="An invite lets one person join the diffx app as a member, with the Google account for that email or, while password signup is on, a password. Invites work whether or not signup is open. Links work once and expire after 48 hours; inviting an email again replaces its pending link. To make someone an admin, promote their account on Accounts."
    >
      <form className="console-form mb-8" onSubmit={create}>
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
      {created && (
        <OneTimeLink
          title="Send this invite link"
          url={created.url}
          onDismiss={() => setCreated(null)}
        >
          It works once, for {created.invite.email}, until{" "}
          {new Date(created.invite.expiresAt).toLocaleString()}.
        </OneTimeLink>
      )}
      <h2 className="mb-2 text-md font-semibold">Sent invites</h2>
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
      {invites.items !== null && invites.items.length === 0 && (
        <p className={prose}>No invites yet.</p>
      )}
      {invites.items !== null && invites.items.length > 0 && (
        <ul className="border-t border-separator">
          {invites.items.map((invite) => (
            <li key={invite.id} className={row}>
              <div className="min-w-0 wrap-anywhere">
                <strong className="flex min-h-control-compact flex-wrap items-center gap-2 font-medium">
                  {invite.email}
                </strong>
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
    </Page>
  );
}
