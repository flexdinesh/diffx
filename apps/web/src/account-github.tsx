import {
  api,
  errorDetail,
  type ApiGitHubConnection,
  type ApiGitHubRepository,
} from "@diffx/api";
import {
  BellIcon,
  BellOffIcon,
  ExternalLinkIcon,
  LockIcon,
} from "lucide-react";
import { useEffect, useState } from "react";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { ErrorState } from "@diffx/ui/components/error-state";
import { LinkButton } from "@diffx/ui/components/link-button";
import { Button } from "@diffx/ui/components/ui/button";
import { Checkbox } from "@diffx/ui/components/ui/checkbox";
import { Toggle } from "@diffx/ui/components/ui/toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@diffx/ui/components/ui/tooltip";
import { RowActions, SettingsSection } from "./settings-layout.tsx";

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

/**
 * Whether GitHub can produce pull requests for this account: not set up on
 * the server, no followed repository, or at least one followed.
 */
export type GitHubFollowing = "unavailable" | "none" | "following";

type Connection =
  | { status: "loading" }
  | { status: "unconfigured" }
  | { status: "error"; message: string }
  | { status: "ready"; connection: ApiGitHubConnection };

/**
 * Connect GitHub and follow repositories whose pull requests appear as
 * reviews. Absent when the server has no GitHub App configured.
 */
export function AccountGitHub({
  notify = false,
  primary = true,
  onFollowingChange,
}: {
  /** Shows each followed repository's notification bell. */
  notify?: boolean;
  /** Whether Connect is the page's primary action; false while another form is open. */
  primary?: boolean;
  onFollowingChange?: (following: GitHubFollowing) => void;
}) {
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
  const unconfigured = connection.status === "unconfigured";
  const inactive =
    connection.status === "ready" &&
    !(
      connection.connection.connected &&
      connection.connection.state === "active"
    );
  useEffect(() => {
    if (unconfigured) onFollowingChange?.("unavailable");
    else if (inactive) onFollowingChange?.("none");
  }, [unconfigured, inactive, onFollowingChange]);
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
    <SettingsSection
      id="github"
      description="Review pull requests from repositories you follow. Each appears with your other reviews of its repository."
    >
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
      {current?.connected && (
        <div className="mb-4 flex items-start justify-between gap-4">
          {active ? (
            <p className="min-w-0 leading-copy text-secondary wrap-anywhere">
              Connected as <strong>@{current.login}</strong>. Pull requests of
              followed repositories update from GitHub events.
            </p>
          ) : (
            <p
              className="min-w-0 leading-copy text-secondary wrap-anywhere"
              role="status"
            >
              GitHub authorization for <strong>@{current.login}</strong>{" "}
              {current.state === "revoked" ? "was revoked" : "expired"}. Connect
              again to keep receiving pull requests.
            </p>
          )}
          <RowActions
            label={`Actions for @${current.login}`}
            disabled={busy}
            actions={[
              {
                label: "Disconnect…",
                onSelect: () => {
                  setError("");
                  setDisconnecting(true);
                },
              },
            ]}
          />
        </div>
      )}
      {error && <ErrorState message={error} className="mb-4" touch />}
      {current && (
        <div className="mb-4 flex flex-wrap gap-2">
          {!active && (
            <Button
              variant={primary ? "default" : "outline"}
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
      {active && (
        <FollowedRepositories
          notify={notify}
          onFollowingChange={onFollowingChange}
        />
      )}
    </SettingsSection>
  );
}

function FollowedRepositories({
  notify,
  onFollowingChange,
}: {
  notify: boolean;
  onFollowingChange: ((following: GitHubFollowing) => void) | undefined;
}) {
  const [repositories, setRepositories] = useState<ApiGitHubRepository[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [error, setError] = useState("");
  const [pending, setPending] = useState<number | null>(null);
  const [resyncing, setResyncing] = useState(false);
  const [notifying, setNotifying] = useState<ReadonlySet<number>>(new Set());
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
              : [{ ...item, followed: follow, lost: false, notify: follow }],
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
  /** Optimistic: the bell flips at once and rolls back if the server refuses. */
  async function setNotify(repository: ApiGitHubRepository, notify: boolean) {
    if (notifying.has(repository.id)) return;
    const update = (value: boolean) =>
      setRepositories((items) =>
        items.map((item) =>
          item.id === repository.id ? { ...item, notify: value } : item,
        ),
      );
    setNotifying((ids) => new Set(ids).add(repository.id));
    setError("");
    setStatus("");
    update(notify);
    try {
      const { response, error } = await api.PATCH(
        "/api/v2/github/follows/{repositoryId}",
        {
          params: { path: { repositoryId: repository.id } },
          body: { notify },
        },
      );
      if (!response.ok)
        throw new Error(
          errorDetail(
            error,
            `Unable to update notifications for ${repository.fullName}. Try again.`,
          ),
        );
      setStatus(
        notify
          ? `Notifications on for ${repository.fullName}.`
          : `Notifications off for ${repository.fullName}.`,
      );
    } catch (failure: unknown) {
      update(!notify);
      setError(
        errorDetail(
          failure,
          `Unable to update notifications for ${repository.fullName}. Try again.`,
        ),
      );
    } finally {
      setNotifying((ids) => {
        const next = new Set(ids);
        next.delete(repository.id);
        return next;
      });
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
  useEffect(() => {
    if (loaded) onFollowingChange?.(following ? "following" : "none");
  }, [loaded, following, onFollowingChange]);
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
                className="flex items-center gap-2 border-b border-separator py-1"
              >
                <Checkbox
                  id={id}
                  checked={repository.followed}
                  disabled={pending !== null}
                  onCheckedChange={(checked) =>
                    void setFollowed(repository, checked)
                  }
                />
                <label
                  htmlFor={id}
                  className="flex min-h-control-small min-w-0 flex-1 items-center wrap-anywhere max-md:min-h-touch pointer-coarse:min-h-touch"
                >
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
                {notify && repository.followed && (
                  <Tooltip>
                    <TooltipTrigger
                      render={
                        <Toggle
                          size="icon-sm"
                          touch
                          aria-label={`Notify for ${repository.fullName}`}
                          pressed={repository.notify}
                          aria-busy={notifying.has(repository.id)}
                          onPressedChange={(pressed) =>
                            void setNotify(repository, pressed)
                          }
                        />
                      }
                    >
                      {repository.notify ? (
                        <BellIcon aria-hidden="true" />
                      ) : (
                        <BellOffIcon aria-hidden="true" />
                      )}
                    </TooltipTrigger>
                    <TooltipContent>
                      Notify for {repository.fullName}
                    </TooltipContent>
                  </Tooltip>
                )}
              </li>
            );
          })}
        </ul>
      )}
      <p className={status ? prose : undefined} role="status">
        {status}
      </p>
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
