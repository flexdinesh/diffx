import {
  api,
  errorDetail,
  readAccountTokens,
  readCreatedToken,
  type ApiAccountToken,
} from "@diffx/api";
import { useEffect, useState, type FormEvent } from "react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Button } from "@diffx/ui/components/ui/button";
import { Input } from "@diffx/ui/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@diffx/ui/components/ui/select";

const purposes: { value: "collector" | "api"; label: string }[] = [
  { value: "collector", label: "Collector" },
  { value: "api", label: "Review API / MCP" },
];
const expiries: { value: 30 | 90 | 365; label: string }[] = [
  { value: 30, label: "30 days" },
  { value: 90, label: "90 days" },
  { value: 365, label: "365 days" },
];
const prose = "mb-4 leading-copy text-secondary";

function date(value: number) {
  return new Date(value).toLocaleDateString();
}

export function AccountTokens() {
  const [tokens, setTokens] = useState<ApiAccountToken[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [name, setName] = useState("");
  const [purpose, setPurpose] = useState<"collector" | "api">("collector");
  const [expiresInDays, setExpiry] = useState<30 | 90 | 365>(90);
  const [created, setCreated] = useState<{
    token: ApiAccountToken;
    secret: string;
  } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [copyStatus, setCopyStatus] = useState("");
  const [revoking, setRevoking] = useState<ApiAccountToken | null>(null);
  const [revokeError, setRevokeError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    void api
      .GET("/api/v2/account/tokens", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data)
          throw new Error(
            errorDetail(error, "Unable to load API tokens. Try again."),
          );
        setTokens(readAccountTokens(data));
        setLoaded(true);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(
            errorDetail(failure, "Unable to load API tokens. Try again."),
          );
      });
    return () => controller.abort();
  }, [attempt]);
  async function create(event: FormEvent) {
    event.preventDefault();
    if (busy) return;
    setBusy(true);
    setError("");
    setCreated(null);
    setCopyStatus("");
    try {
      const { data, error } = await api.POST("/api/v2/account/tokens", {
        body: { name, purpose, expiresInDays },
      });
      if (!data)
        throw new Error(
          errorDetail(error, "Unable to create token. Try again."),
        );
      const created = readCreatedToken(data);
      setCreated(created);
      setTokens((value) => [...value, created.token]);
      setName("");
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to create token. Try again."));
    } finally {
      setBusy(false);
    }
  }
  async function revoke(id: string) {
    setBusy(true);
    setRevokeError("");
    try {
      const { response, error } = await api.DELETE(
        "/api/v2/account/tokens/{tokenId}",
        { params: { path: { tokenId: id } } },
      );
      if (!response.ok)
        throw new Error(
          errorDetail(error, "Unable to revoke token. Try again."),
        );
      setTokens((value) => value.filter((token) => token.id !== id));
      if (created?.token.id === id) setCreated(null);
      setRevoking(null);
    } catch (failure: unknown) {
      setRevokeError(
        errorDetail(failure, "Unable to revoke token. Try again."),
      );
    } finally {
      setBusy(false);
    }
  }
  const configuration = created
    ? JSON.stringify(
        { remote: { url: window.location.origin, token: created.secret } },
        null,
        2,
      )
    : "";
  async function copy() {
    try {
      await navigator.clipboard.writeText(
        created?.token.purpose === "collector"
          ? configuration
          : (created?.secret ?? ""),
      );
      setCopyStatus("Copied");
    } catch {
      setCopyStatus("Copy unavailable. Select and copy the text below.");
    }
  }
  return (
    <section
      className="mt-8 border-t border-separator pt-6"
      aria-labelledby="tokens-title"
    >
      <h2 id="tokens-title" className="mb-2 text-md font-semibold">
        API tokens
      </h2>
      <p className={prose}>
        Connect collectors or review API clients to your account. Secrets appear
        once.
      </p>
      {error && (
        <ErrorState
          message={error}
          className="mb-4"
          touch
          {...(!loaded && {
            onRetry: () => setAttempt((value) => value + 1),
          })}
        />
      )}
      {!loaded && !error && (
        <p className={prose} role="status">
          Loading tokens…
        </p>
      )}
      {loaded && (
        <>
          {tokens.length === 0 ? (
            <p className={prose}>
              No tokens yet. Create a collector token to publish your first
              review.
            </p>
          ) : (
            <ul className="mb-6">
              {tokens.map((token) => (
                <li
                  key={token.id}
                  className="flex items-center justify-between gap-4 border-b border-separator py-3"
                >
                  <div className="min-w-0 wrap-anywhere">
                    <strong className="block font-medium">{token.name}</strong>
                    <span className="mt-1 block text-sm text-muted">
                      {token.purpose === "collector"
                        ? "Collector"
                        : "Review API"}{" "}
                      · Created {date(token.createdAt)} ·{" "}
                      {token.expiresAt <= Date.now() ? "Expired" : "Expires"}{" "}
                      {date(token.expiresAt)}
                    </span>
                    <span className="mt-1 block text-sm text-muted">
                      {token.lastUsedAt === null
                        ? "Never used"
                        : `Last used ${date(token.lastUsedAt)}`}
                    </span>
                  </div>
                  <Button
                    variant="destructive"
                    touch
                    disabled={busy}
                    aria-label={`Revoke ${token.name}`}
                    onClick={() => {
                      setRevokeError("");
                      setRevoking(token);
                    }}
                  >
                    Revoke
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <ConfirmDialog
            open={revoking !== null}
            onOpenChange={(open) => {
              if (!open) setRevoking(null);
            }}
            title={`Revoke ${revoking?.name ?? "token"}?`}
            description="Clients using this token lose access immediately. This cannot be undone."
            error={revokeError}
            confirmLabel="Revoke token"
            busyLabel="Revoking…"
            busy={busy}
            onConfirm={() => {
              if (revoking) void revoke(revoking.id);
            }}
          />
          <form className="account-form" onSubmit={create}>
            <label htmlFor="token-name">Token name</label>
            <Input
              id="token-name"
              required
              maxLength={64}
              placeholder="My laptop"
              touch
              value={name}
              onChange={(event) => setName(event.target.value)}
              disabled={busy}
            />
            <label htmlFor="token-purpose">Access</label>
            <Select
              items={purposes}
              value={purpose}
              disabled={busy}
              onValueChange={(value) =>
                setPurpose(value === "api" ? "api" : "collector")
              }
            >
              <SelectTrigger id="token-purpose" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {purposes.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-sm leading-copy text-secondary">
              {purpose === "collector"
                ? "Upload changes and check their ingestion status."
                : "Read and update your reviews through the API or MCP."}
            </p>
            <label htmlFor="token-expiry">Expires after</label>
            <Select
              items={expiries}
              value={expiresInDays}
              disabled={busy}
              onValueChange={(value) =>
                setExpiry(value === 30 ? 30 : value === 365 ? 365 : 90)
              }
            >
              <SelectTrigger id="token-expiry" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {expiries.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              type="submit"
              touch
              className="mt-2 justify-self-start"
              disabled={busy}
              aria-busy={busy}
            >
              Create token
            </Button>
          </form>
        </>
      )}
      {created && (
        <div
          className="mt-6 rounded-control border border-separator bg-panel p-4"
          role="status"
        >
          <h3 className="mb-2 text-md font-semibold">Save your token now</h3>
          <p className={prose}>
            This secret will disappear when you leave this page.
          </p>
          {created.token.purpose === "collector" && (
            <p className={prose}>
              Merge this remote section into your diffx JSON config, then run{" "}
              <code>diffx sync</code>.
            </p>
          )}
          <pre
            tabIndex={0}
            className="my-4 font-mono text-sm whitespace-pre-wrap wrap-anywhere select-all"
          >
            {created.token.purpose === "collector"
              ? configuration
              : created.secret}
          </pre>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" touch onClick={() => void copy()}>
              {created.token.purpose === "collector"
                ? "Copy configuration"
                : "Copy token"}
            </Button>
            <Button variant="ghost" touch onClick={() => setCreated(null)}>
              Dismiss secret
            </Button>
          </div>
          {copyStatus && <p className={prose}>{copyStatus}</p>}
        </div>
      )}
    </section>
  );
}
