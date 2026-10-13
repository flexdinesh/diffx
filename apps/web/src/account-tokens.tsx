import {
  api,
  errorDetail,
  readAccountTokens,
  readCreatedToken,
  type ApiAccountToken,
} from "@diffx/api";
import { useEffect, useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

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
    setError("");
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
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to revoke token. Try again."));
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
    <section className="account-section" aria-labelledby="tokens-title">
      <h2 id="tokens-title">API tokens</h2>
      <p>
        Connect collectors or review API clients to your account. Secrets appear
        once.
      </p>
      {error && (
        <p className="account-error" role="alert">
          {error}
        </p>
      )}
      {!loaded && !error && <p role="status">Loading tokens…</p>}
      {!loaded && error && (
        <Button
          variant="outline"
          onClick={() => setAttempt((value) => value + 1)}
        >
          Retry loading tokens
        </Button>
      )}
      {loaded && (
        <>
          {tokens.length === 0 ? (
            <p>
              No tokens yet. Create a collector token to publish your first
              review.
            </p>
          ) : (
            <ul className="account-token-list">
              {tokens.map((token) => (
                <li key={token.id}>
                  <div>
                    <strong>{token.name}</strong>
                    <span>
                      {token.purpose === "collector"
                        ? "Collector"
                        : "Review API"}{" "}
                      · Created {date(token.createdAt)} ·{" "}
                      {token.expiresAt <= Date.now() ? "Expired" : "Expires"}{" "}
                      {date(token.expiresAt)}
                    </span>
                    <span>
                      {token.lastUsedAt === null
                        ? "Never used"
                        : `Last used ${date(token.lastUsedAt)}`}
                    </span>
                  </div>
                  <Button
                    variant="destructive"
                    disabled={busy}
                    aria-label={`Revoke ${token.name}`}
                    onClick={() => void revoke(token.id)}
                  >
                    Revoke
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <form className="account-form token-form" onSubmit={create}>
            <label htmlFor="token-name">Token name</label>
            <Input
              id="token-name"
              required
              maxLength={64}
              placeholder="My laptop"
              value={name}
              onChange={(event) => setName(event.target.value)}
              disabled={busy}
            />
            <label htmlFor="token-purpose">Access</label>
            <select
              id="token-purpose"
              value={purpose}
              disabled={busy}
              onChange={(event) =>
                setPurpose(event.target.value === "api" ? "api" : "collector")
              }
            >
              <option value="collector">Collector</option>
              <option value="api">Review API / MCP</option>
            </select>
            <p className="account-hint">
              {purpose === "collector"
                ? "Upload changes and check their ingestion status."
                : "Read and update your reviews through the API or MCP."}
            </p>
            <label htmlFor="token-expiry">Expires after</label>
            <select
              id="token-expiry"
              value={expiresInDays}
              disabled={busy}
              onChange={(event) =>
                setExpiry(
                  event.target.value === "30"
                    ? 30
                    : event.target.value === "365"
                      ? 365
                      : 90,
                )
              }
            >
              <option value={30}>30 days</option>
              <option value={90}>90 days</option>
              <option value={365}>365 days</option>
            </select>
            <Button type="submit" disabled={busy} aria-busy={busy}>
              Create token
            </Button>
          </form>
        </>
      )}
      {created && (
        <div className="token-secret" role="status">
          <h3>Save your token now</h3>
          <p>This secret will disappear when you leave this page.</p>
          {created.token.purpose === "collector" && (
            <p>
              Merge this remote section into your diffx JSON config, then run{" "}
              <code>diffx sync</code>.
            </p>
          )}
          <pre tabIndex={0}>
            {created.token.purpose === "collector"
              ? configuration
              : created.secret}
          </pre>
          <div className="account-actions">
            <Button variant="outline" onClick={() => void copy()}>
              {created.token.purpose === "collector"
                ? "Copy configuration"
                : "Copy token"}
            </Button>
            <Button variant="ghost" onClick={() => setCreated(null)}>
              Dismiss secret
            </Button>
          </div>
          {copyStatus && <p>{copyStatus}</p>}
        </div>
      )}
    </section>
  );
}
