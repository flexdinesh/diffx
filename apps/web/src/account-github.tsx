import {
  api,
  errorDetail,
  type ApiGitHubConnection,
  type ApiGitHubRepository,
} from "@diffx/api";
import { ExternalLinkIcon, LockIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ErrorState } from "@/components/error-state";
import { LinkButton } from "@/components/link-button";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

const prose = "mb-4 leading-copy text-secondary";

/** Outcomes the OAuth callback reports through `/settings?github=`. */
const callbackMessages: Record<string, string> = {
  connected: "GitHub connected. Choose repositories to follow.",
  cancelled: "GitHub authorization was cancelled.",
  failed: "GitHub authorization failed. Try connecting again.",
  signin: "Sign in, then connect GitHub again from this browser.",
};

/** Reads and clears the callback outcome so a reload does not repeat it. */
function takeCallbackMessage() {
  const url = new URL(window.location.href);
  const outcome = url.searchParams.get("github");
  if (outcome === null) return "";
  url.searchParams.delete("github");
  window.history.replaceState(window.history.state, "", url);
  return callbackMessages[outcome] ?? "";
}

type Connection =
  | { status: "loading" }
  | { status: "unconfigured" }
  | { status: "error"; message: string }
  | { status: "ready"; connection: ApiGitHubConnection };

/**
 * Connect GitHub and follow repositories whose pull requests appear as
 * reviews. Absent when the server has no GitHub App configured.
 */
export function AccountGitHub() {
  const [connection, setConnection] = useState<Connection>({
    status: "loading",
  });
  const [attempt, setAttempt] = useState(0);
  const [callback] = useState(takeCallbackMessage);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [disconnecting, setDisconnecting] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    void api
      .GET("/api/v2/github/connection", { signal: controller.signal })
      .then(({ data, error, response }) => {
        if (controller.signal.aborted) return;
        if (response.status === 404) {
          setConnection({ status: "unconfigured" });
          return;
        }
        if (!data)
          throw new Error(
            errorDetail(error, "Unable to load GitHub status. Try again."),
          );
        setConnection({ status: "ready", connection: data });
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setConnection({
            status: "error",
            message: errorDetail(
              failure,
              "Unable to load GitHub status. Try again.",
            ),
          });
      });
    return () => controller.abort();
  }, [attempt]);
  if (connection.status === "unconfigured" || connection.status === "loading")
    return null;
  async function connect() {
    setBusy(true);
    setError("");
    try {
      const { data, error } = await api.POST("/api/v2/github/connection");
      if (!data)
        throw new Error(
          errorDetail(error, "Unable to connect GitHub. Try again."),
        );
      window.location.assign(data.authorizeUrl);
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to connect GitHub. Try again."));
      setBusy(false);
    }
  }
  async function disconnect() {
    setBusy(true);
    setError("");
    try {
      const { response, error } = await api.DELETE("/api/v2/github/connection");
      if (!response.ok)
        throw new Error(
          errorDetail(error, "Unable to disconnect GitHub. Try again."),
        );
      setDisconnecting(false);
      setAttempt((value) => value + 1);
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to disconnect GitHub. Try again."));
    } finally {
      setBusy(false);
    }
  }
  const current = connection.status === "ready" ? connection.connection : null;
  const active = current?.connected === true && current.state === "active";
  return (
    <section
      className="mt-8 border-t border-separator pt-6"
      aria-labelledby="github-title"
    >
      <h2 id="github-title" className="mb-2 text-md font-semibold">
        GitHub
      </h2>
      {callback && (
        <p className={prose} role="status">
          {callback}
        </p>
      )}
      {connection.status === "error" && (
        <ErrorState
          message={connection.message}
          className="mb-4"
          touch
          onRetry={() => setAttempt((value) => value + 1)}
        />
      )}
      {current && !current.connected && (
        <p className={prose}>
          Connect GitHub to review pull requests from repositories you follow.
          Each pull request appears with your other reviews of its repository.
        </p>
      )}
      {current?.connected && !active && (
        <p className={prose} role="status">
          GitHub authorization for <strong>@{current.login}</strong>{" "}
          {current.state === "revoked" ? "was revoked" : "expired"}. Connect
          again to keep receiving pull requests.
        </p>
      )}
      {active && (
        <p className={prose}>
          Connected as <strong>@{current.login}</strong>. Pull requests of
          followed repositories update from GitHub events.
        </p>
      )}
      {error && <ErrorState message={error} className="mb-4" touch />}
      {current && (
        <div className="mb-4 flex flex-wrap gap-2">
          {!active && (
            <Button
              touch
              disabled={busy}
              aria-busy={busy}
              onClick={() => void connect()}
            >
              {current.connected ? "Connect GitHub again" : "Connect GitHub"}
            </Button>
          )}
          <LinkButton
            variant="outline"
            touch
            href={current.installUrl}
            external
          >
            Install on repositories
            <ExternalLinkIcon className="size-icon-sm" aria-hidden="true" />
          </LinkButton>
          {current.connected && (
            <Button
              variant="destructive"
              touch
              disabled={busy}
              onClick={() => {
                setError("");
                setDisconnecting(true);
              }}
            >
              Disconnect
            </Button>
          )}
        </div>
      )}
      <ConfirmDialog
        open={disconnecting}
        onOpenChange={(open) => {
          if (!open) setDisconnecting(false);
        }}
        title="Disconnect GitHub?"
        description="You stop following every repository. Existing pull request reviews stay until they expire."
        error={error}
        confirmLabel="Disconnect GitHub"
        busyLabel="Disconnecting…"
        busy={busy}
        onConfirm={() => void disconnect()}
      />
      {active && <FollowedRepositories />}
    </section>
  );
}

function FollowedRepositories() {
  const [repositories, setRepositories] = useState<ApiGitHubRepository[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState("");
  const [pending, setPending] = useState<number | null>(null);
  const [resyncing, setResyncing] = useState(false);
  const [status, setStatus] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setError("");
    void api
      .GET("/api/v2/github/repositories", { signal: controller.signal })
      .then(({ data, error }) => {
        if (controller.signal.aborted) return;
        if (!data)
          throw new Error(
            errorDetail(error, "Unable to load repositories. Try again."),
          );
        setRepositories(data.repositories);
        setLoaded(true);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted)
          setError(
            errorDetail(failure, "Unable to load repositories. Try again."),
          );
      });
    return () => controller.abort();
  }, [attempt]);
  async function setFollowed(repository: ApiGitHubRepository, follow: boolean) {
    setPending(repository.id);
    setError("");
    setStatus("");
    try {
      const params = { params: { path: { repositoryId: repository.id } } };
      const { response, error } = follow
        ? await api.PUT("/api/v2/github/follows/{repositoryId}", params)
        : await api.DELETE("/api/v2/github/follows/{repositoryId}", params);
      if (!response.ok)
        throw new Error(
          errorDetail(
            error,
            `Unable to ${follow ? "follow" : "unfollow"} ${repository.fullName}. Try again.`,
          ),
        );
      setRepositories((value) =>
        value.flatMap((item) =>
          item.id !== repository.id
            ? [item]
            : !follow && item.lost
              ? []
              : [{ ...item, followed: follow, lost: false }],
        ),
      );
      setStatus(
        follow
          ? `Following ${repository.fullName}. Its open pull requests appear shortly.`
          : `Stopped following ${repository.fullName}.`,
      );
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to update. Try again."));
    } finally {
      setPending(null);
    }
  }
  async function resync() {
    setResyncing(true);
    setError("");
    setStatus("");
    try {
      const { response, error } = await api.POST("/api/v2/github/resync");
      if (!response.ok)
        throw new Error(errorDetail(error, "Unable to resync. Try again."));
      setStatus("Refreshing pull requests of followed repositories.");
    } catch (failure: unknown) {
      setError(errorDetail(failure, "Unable to resync. Try again."));
    } finally {
      setResyncing(false);
    }
  }
  const following = repositories.some((repository) => repository.followed);
  return (
    <section aria-labelledby="github-repositories-title">
      <h3
        id="github-repositories-title"
        className="mb-2 text-base font-semibold"
      >
        Repositories
      </h3>
      <p className={prose}>
        Follow repositories to review their open pull requests, drafts included.
        Closed and merged pull requests leave the Reviews column.
      </p>
      {error && (
        <ErrorState
          message={error}
          className="mb-4"
          touch
          {...(!loaded && { onRetry: () => setAttempt((value) => value + 1) })}
        />
      )}
      {!loaded && !error && (
        <p className={prose} role="status">
          Loading repositories…
        </p>
      )}
      {loaded && repositories.length === 0 && (
        <p className={prose}>
          No repositories yet. Install the app on repositories you can read,
          then reload this page.
        </p>
      )}
      {loaded && repositories.length > 0 && (
        <ul className="mb-4" aria-label="GitHub repositories">
          {repositories.map((repository) => {
            const id = `github-repository-${repository.id}`;
            return (
              <li
                key={repository.id}
                className="flex items-center gap-2 border-b border-separator py-2"
              >
                <Checkbox
                  id={id}
                  checked={repository.followed}
                  disabled={pending !== null}
                  onCheckedChange={(checked) =>
                    void setFollowed(repository, checked)
                  }
                />
                <label htmlFor={id} className="min-w-0 flex-1 wrap-anywhere">
                  {repository.fullName}
                </label>
                {repository.private && (
                  <LockIcon
                    className="size-icon-sm text-muted"
                    role="img"
                    aria-label="Private"
                  />
                )}
                {repository.lost && (
                  <span className="text-sm text-warning">Access lost</span>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {status && (
        <p className={prose} role="status">
          {status}
        </p>
      )}
      {following && (
        <Button
          variant="outline"
          touch
          disabled={resyncing}
          aria-busy={resyncing}
          onClick={() => void resync()}
        >
          Resync pull requests
        </Button>
      )}
    </section>
  );
}
