import { api, errorDetail, type ConsoleAccount } from "@diffx/console-api";
import { EllipsisIcon } from "lucide-react";
import { useState } from "react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { OneTimeLink, Page, date, prose, row, useList } from "./page.tsx";
import { useAdmin } from "./session.tsx";

async function loadAccounts(signal: AbortSignal) {
  const { data, error } = await api.GET("/api/v1/accounts", { signal });
  if (!data)
    throw new Error(errorDetail(error, "Unable to load accounts. Try again."));
  return data.accounts;
}

type Action = "promote" | "demote" | "enable" | "disable" | "delete" | "reset";
type Confirming = { kind: "disable" | "delete"; account: ConsoleAccount };
type ResetLink = { account: ConsoleAccount; url: string; expiresAt: number };

const done: Record<Exclude<Action, "reset">, (username: string) => string> = {
  promote: (username) => `${username} is now an admin.`,
  demote: (username) => `${username} is no longer an admin.`,
  enable: (username) => `${username} can sign in again.`,
  disable: (username) => `${username} is disabled.`,
  delete: (username) => `${username} is deleted.`,
};

function request(action: Action, accountId: string) {
  const params = { params: { path: { accountId } } };
  switch (action) {
    case "promote":
    case "demote":
      return api.PUT("/api/v1/accounts/{accountId}/admin", {
        ...params,
        body: { admin: action === "promote" },
      });
    case "enable":
      return api.POST("/api/v1/accounts/{accountId}/enable", params);
    case "disable":
      return api.POST("/api/v1/accounts/{accountId}/disable", params);
    case "delete":
      return api.DELETE("/api/v1/accounts/{accountId}", params);
    case "reset":
      return api.POST("/api/v1/accounts/{accountId}/password-reset", params);
  }
}

/** Every account on the server, with role, access and recovery actions. */
export function Accounts() {
  const admin = useAdmin();
  const accounts = useList(loadAccounts, "Unable to load accounts. Try again.");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [confirming, setConfirming] = useState<Confirming | null>(null);
  const [confirmError, setConfirmError] = useState("");
  const [reset, setReset] = useState<ResetLink | null>(null);
  async function apply(action: Action, account: ConsoleAccount) {
    setBusy(true);
    setError("");
    setStatus("");
    setConfirmError("");
    const failure = `Unable to ${action === "reset" ? "create a reset link for" : action} ${account.username}. Try again.`;
    try {
      const { data, error, response } = await request(action, account.id);
      if (!response.ok) throw new Error(errorDetail(error, failure));
      if (action === "reset") {
        if (!data) throw new Error(failure);
        setReset({ account, ...data });
      } else {
        setStatus(done[action](account.username));
        if (reset?.account.id === account.id) setReset(null);
      }
      setConfirming(null);
      accounts.reload();
    } catch (failed: unknown) {
      if (confirming) setConfirmError(errorDetail(failed, failure));
      else setError(errorDetail(failed, failure));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Page
      title="Accounts"
      description="Everyone with a diffx account on this server. Admins also sign in to this console."
    >
      {reset && (
        <OneTimeLink
          title={`Send this reset link to ${reset.account.username}`}
          url={reset.url}
          onDismiss={() => setReset(null)}
        >
          It opens {reset.account.admin ? "this console" : "the diffx app"},
          works once and expires {new Date(reset.expiresAt).toLocaleString()}.
          Setting a password signs out their browser sessions; API tokens remain
          active.
        </OneTimeLink>
      )}
      {accounts.error && (
        <ErrorState
          message={accounts.error}
          className="mb-4"
          touch
          onRetry={accounts.reload}
        />
      )}
      {error && <ErrorState message={error} className="mb-4" />}
      {accounts.items === null && !accounts.error && (
        <p className={prose} role="status">
          Loading accounts…
        </p>
      )}
      {accounts.items !== null && (
        <ul className="border-t border-separator">
          {accounts.items.map((account) => (
            <li key={account.id} className={row}>
              <div className="min-w-0 wrap-anywhere">
                <strong className="flex min-h-control-compact flex-wrap items-center gap-2 font-medium">
                  <span>{account.username}</span>
                  {account.id === admin.id && (
                    <Badge variant="status">You</Badge>
                  )}
                  {account.admin && (
                    <Badge variant="status" tone="accent">
                      Admin
                    </Badge>
                  )}
                  {account.disabled && (
                    <Badge variant="status" tone="warning">
                      Disabled
                    </Badge>
                  )}
                </strong>
                <span className="mt-1 block text-sm text-muted">
                  {account.method === "google"
                    ? `Google · ${account.email ?? "no email"}`
                    : "Password"}{" "}
                  · Joined {date(account.createdAt)}
                </span>
              </div>
              {account.id !== admin.id && (
                <AccountActions
                  account={account}
                  busy={busy}
                  onAction={(action) => {
                    if (action === "disable" || action === "delete") {
                      setConfirmError("");
                      setConfirming({ kind: action, account });
                    } else void apply(action, account);
                  }}
                />
              )}
            </li>
          ))}
        </ul>
      )}
      <p className="mt-4 text-sm leading-copy text-secondary" role="status">
        {status}
      </p>
      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(open) => {
          if (!open) setConfirming(null);
        }}
        title={
          confirming?.kind === "delete"
            ? `Delete ${confirming.account.username}?`
            : `Disable ${confirming?.account.username ?? "account"}?`
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
          if (confirming) void apply(confirming.kind, confirming.account);
        }}
      />
    </Page>
  );
}

function AccountActions({
  account,
  busy,
  onAction,
}: {
  account: ConsoleAccount;
  busy: boolean;
  onAction: (action: Action) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            touch
            disabled={busy}
            aria-label={`Actions for ${account.username}`}
          />
        }
      >
        <EllipsisIcon aria-hidden="true" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          onClick={() => onAction(account.admin ? "demote" : "promote")}
        >
          {account.admin ? "Remove admin" : "Make admin"}
        </DropdownMenuItem>
        {account.method === "password" && (
          <DropdownMenuItem onClick={() => onAction("reset")}>
            Create reset link
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          onClick={() => onAction(account.disabled ? "enable" : "disable")}
        >
          {account.disabled ? "Enable" : "Disable…"}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => onAction("delete")}>
          Delete…
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
