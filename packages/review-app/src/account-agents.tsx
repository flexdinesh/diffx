import {
  api,
  errorDetail,
  readConnectedAgents,
  type ApiConnectedAgent,
} from "@diffx/api";
import { useEffect, useState } from "react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { ErrorState } from "@diffx/ui/components/error-state";
import { Badge } from "@diffx/ui/components/ui/badge";
import { RowActions, SettingsSection } from "./settings-layout.tsx";

const prose = "mb-4 leading-copy text-secondary";
const mcpDocs = "https://docs.diffx.app/mcp";

function date(value: number) {
  return new Date(value).toLocaleDateString();
}

/** What a grant allows, in words. */
export function accessWords(scopes: ApiConnectedAgent["scopes"]) {
  return scopes.includes("comments:write")
    ? "Reads reviews and adds comments"
    : "Reads reviews";
}

/**
 * MCP clients the account connected with OAuth. Each row names the app, how
 * it was identified and what it may do; Disconnect… ends its access at once.
 */
export function AccountAgents() {
  const [agents, setAgents] = useState<ApiConnectedAgent[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [disconnecting, setDisconnecting] = useState<ApiConnectedAgent | null>(
    null,
  );
  const [disconnectError, setDisconnectError] = useState("");
  const [status, setStatus] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    void api
      .GET("/api/v2/account/agents", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data)
          throw new Error(
            errorDetail(error, "Unable to load connected agents. Try again."),
          );
        setAgents(readConnectedAgents(data));
        setLoaded(true);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(
            errorDetail(failure, "Unable to load connected agents. Try again."),
          );
      });
    return () => controller.abort();
  }, [attempt]);
  async function disconnect(agent: ApiConnectedAgent) {
    setBusy(true);
    setDisconnectError("");
    try {
      const { response, error } = await api.DELETE(
        "/api/v2/account/agents/{agentId}",
        { params: { path: { agentId: agent.id } } },
      );
      if (!response.ok && response.status !== 404)
        throw new Error(errorDetail(error, "Unable to disconnect. Try again."));
      setAgents((value) => value.filter((item) => item.id !== agent.id));
      setStatus(`Disconnected ${agent.name}.`);
      setDisconnecting(null);
    } catch (failure: unknown) {
      setDisconnectError(
        errorDetail(failure, "Unable to disconnect. Try again."),
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <SettingsSection
      id="agents"
      description="Apps you connected with OAuth, such as Claude or Cursor. Disconnecting ends their access at once."
    >
      {error && !loaded && (
        <ErrorState
          message={error}
          className="mb-4"
          touch
          onRetry={() => setAttempt((value) => value + 1)}
        />
      )}
      {!loaded && !error && (
        <p className={prose} role="status">
          Loading connected agents…
        </p>
      )}
      {loaded &&
        (agents.length === 0 ? (
          <p className={prose}>
            No connected agents.{" "}
            <a
              className="text-accent underline underline-offset-2"
              href={mcpDocs}
              target="_blank"
              rel="noreferrer"
            >
              Connect one over MCP
            </a>
            .
          </p>
        ) : (
          <ul className="mb-4" aria-label="Connected agents">
            {agents.map((agent) => (
              <li
                key={agent.id}
                className="flex items-center justify-between gap-4 border-b border-separator py-3 first:pt-0"
              >
                <div className="min-w-0 wrap-anywhere">
                  <span className="flex flex-wrap items-center gap-2">
                    <strong className="font-medium">{agent.name}</strong>
                    {agent.verified ? (
                      <Badge variant="status" tone="success">
                        Verified
                      </Badge>
                    ) : (
                      <Badge variant="status" tone="warning">
                        Unverified
                      </Badge>
                    )}
                  </span>
                  <span className="mt-1 block text-sm text-muted">
                    {agent.host ? `${agent.host} · ` : ""}
                    {accessWords(agent.scopes)}
                  </span>
                  <span className="mt-1 block text-sm text-muted">
                    Connected {date(agent.createdAt)} ·{" "}
                    {agent.lastUsedAt === null
                      ? "Never used"
                      : `Last used ${date(agent.lastUsedAt)}`}
                  </span>
                </div>
                <RowActions
                  label={`Actions for ${agent.name}`}
                  disabled={busy}
                  actions={[
                    {
                      label: "Disconnect…",
                      onSelect: () => {
                        setDisconnectError("");
                        setDisconnecting(agent);
                      },
                    },
                  ]}
                />
              </li>
            ))}
          </ul>
        ))}
      <p className="text-secondary" role="status">
        {status}
      </p>
      <ConfirmDialog
        open={disconnecting !== null}
        onOpenChange={(open) => {
          if (!open) setDisconnecting(null);
        }}
        title={`Disconnect ${disconnecting?.name ?? "agent"}?`}
        description="It loses access to your reviews immediately. Connect it again from the app to restore access."
        error={disconnectError}
        confirmLabel="Disconnect"
        busyLabel="Disconnecting…"
        busy={busy}
        onConfirm={() => {
          if (disconnecting) void disconnect(disconnecting);
        }}
      />
    </SettingsSection>
  );
}
