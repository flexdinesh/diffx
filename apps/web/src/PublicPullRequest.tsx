import { EyeIcon, RefreshCwIcon, XIcon } from "lucide-react";
import {
  createContext,
  useContext,
  useEffect,
  useMemo,
  useState,
  type MouseEvent,
  type ReactNode,
} from "react";
import { ContextIcon } from "@diffx/ui/components/context-icon";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { LinkButton } from "@diffx/ui/components/link-button";
import { Notice } from "@diffx/ui/components/notice";
import { Button } from "@diffx/ui/components/ui/button";
import { save, saved } from "@diffx/ui/lib/storage";
import { navigate, useIdentity } from "./identity-context.tsx";
import { Navigation } from "./Navigation.tsx";
import { NewerNotice, useNow } from "./ProjectsNav.tsx";
import { pullRequestOf } from "./project-nav.ts";
import { PullRequestSummary } from "./PullRequestBanner.tsx";
import {
  elapsedLabel,
  failureMessage,
  fetchedLabel,
  gitHubUrl,
  notPublic,
  pullRequestName,
  retryLabel,
  type PullRequestRef,
} from "./public-pull-request.ts";
import {
  SessionProvider,
  useCatalog,
  useSessionShell,
} from "./session-context.tsx";
import { publicPreviewPolicy } from "./shell-policy.ts";
import { SidebarToggle, Topbar } from "./Topbar.tsx";
import {
  usePublicPullRequest,
  type PublicPreview,
} from "./use-public-pull-request.ts";
import { ShellPolicyContext } from "./use-shell-policy.ts";
import { WorkspaceLoading } from "./WorkspaceLoading.tsx";

const noticeKey = "public-preview-notice";
const followPage = "/settings/github";
/** After this long, the status explains that first fetches of large repositories are slow. */
const slowFetchMilliseconds = 10_000;

/** In-app navigation for links that must stay real links. */
function follow(event: MouseEvent<HTMLAnchorElement>) {
  event.preventDefault();
  navigate(event.currentTarget.getAttribute("href") ?? "/");
}

const PreviewContext = createContext<{
  pull: PullRequestRef;
  preview: PublicPreview;
} | null>(null);

function usePreview() {
  const value = useContext(PreviewContext);
  if (!value) throw new Error("Public preview parts require PublicPullRequest");
  return value;
}

/**
 * A read-only preview of a public GitHub pull request in a standalone shell,
 * for signed-in and signed-out readers alike: no Reviews column, search,
 * comments, review marks or copy. `children` is the review, shown once its
 * snapshot is ready, in a `PublicLayout`; until then the shell shows what
 * the server is doing.
 */
export function PublicPullRequest({
  pull,
  children,
}: {
  pull: PullRequestRef;
  children?: ReactNode;
}) {
  const { state } = useIdentity();
  const preview = usePublicPullRequest(pull);
  const policy = useMemo(
    () =>
      state.status === "ready" ? publicPreviewPolicy(state.bootstrap) : null,
    [state],
  );
  const value = useMemo(() => ({ pull, preview }), [pull, preview]);
  return (
    <ShellPolicyContext value={policy}>
      <PreviewContext value={value}>
        <SessionProvider
          source={{ kind: "public", contextId: preview.viewedId }}
        >
          <PublicShell>{children}</PublicShell>
        </SessionProvider>
      </PreviewContext>
    </ShellPolicyContext>
  );
}

function PublicShell({ children }: { children: ReactNode }) {
  const { preview } = usePreview();
  const { ready, statusError } = useSessionShell();
  if (ready) return children;
  const failed = Boolean(statusError) || failure(preview) !== null;
  return (
    <PublicLayout ready={false} navigation={<Navigation loading={!failed} />}>
      <PublicStatus />
    </PublicLayout>
  );
}

/**
 * The preview's shell around its review, or its loading and failure state:
 * header, navigation island, then the banner and notices above `children`.
 */
export function PublicLayout({
  ready,
  navigation,
  overlay,
  children,
}: {
  ready: boolean;
  navigation: ReactNode;
  /** The mobile drawer backdrop, once the review is ready. */
  overlay?: ReactNode;
  children?: ReactNode;
}) {
  return (
    <div className="app-shell">
      <div className="app-main">
        {overlay}
        <PublicHeader ready={ready} />
        <div className="workbench">
          {navigation}
          <div className="workspace">
            <PublicBanner />
            <NewerPreviewNotice />
            <PreviewNotice />
            {children}
          </div>
        </div>
      </div>
    </div>
  );
}

function useSignedIn() {
  const { state } = useIdentity();
  return state.status === "ready" && state.bootstrap.user !== null;
}

/** Brand, the static pull request identity, then Sign in or the account. */
function PublicHeader({ ready }: { ready: boolean }) {
  const { pull } = usePreview();
  const signedIn = useSignedIn();
  const name = pullRequestName(pull);
  return (
    <Topbar
      lead={ready && <SidebarToggle />}
      identity={
        <div
          className="context-identity"
          title={`${name} pull request #${pull.number}`}
        >
          <span className="context-identity-static">
            <ContextIcon size="md" />
            <span className="context-identity-name">{name}</span>
            <span className="context-identity-branch font-mono">
              PR #{pull.number}
            </span>
          </span>
        </div>
      }
      actions={
        !signedIn && (
          <LinkButton variant="shell" touch href="/signin" onClick={follow}>
            Sign in
          </LinkButton>
        )
      }
    >
      {ready && (
        <h1 id="changes-title" className="sr-only">
          {`Pull request preview: ${name} #${pull.number}`}
        </h1>
      )}
    </Topbar>
  );
}

/**
 * The pull request banner as soon as its metadata is known, with when the
 * snapshot was fetched and a manual refresh.
 */
function PublicBanner() {
  const { pull, preview } = usePreview();
  const { current } = useCatalog();
  const now = useNow();
  const status = preview.preview;
  const pullRequest =
    (current && pullRequestOf(current)) ?? status?.pullRequest ?? null;
  if (!pullRequest) return null;
  const snapshot = preview.viewedId !== null;
  const busy = preview.refreshing || (snapshot && preview.pending);
  const announced =
    preview.feedback || (busy && snapshot ? "Fetching from GitHub…" : "");
  const fetched =
    status?.fetchedAt && status.contextId === preview.viewedId
      ? fetchedLabel(status.fetchedAt, now)
      : "";
  return (
    <PullRequestSummary
      pull={pullRequest}
      actions={
        snapshot && (
          <>
            <span className="visually-hidden" role="status">
              {announced}
            </span>
            {(announced || fetched) && (
              <span className="text-xs text-muted" aria-hidden="true">
                {announced || fetched}
              </span>
            )}
            <IconButton
              size="icon-sm"
              label={`Refresh pull request #${pull.number}`}
              hint="Refresh pull request"
              aria-busy={busy}
              disabled={busy}
              onClick={() => void preview.refresh()}
            >
              <RefreshCwIcon
                className={busy ? "motion-safe:animate-spinner" : undefined}
                aria-hidden="true"
              />
            </IconButton>
          </>
        )
      }
    />
  );
}

/** A newer snapshot is ready; the open one stays until the reader asks. */
function NewerPreviewNotice() {
  const { pull, preview } = usePreview();
  const now = useNow();
  const fetchedAt = preview.preview?.fetchedAt;
  return (
    <NewerNotice onView={preview.newerId ? preview.viewLatest : undefined}>
      {`Newer snapshot of ${pullRequestName(pull)} #${pull.number}`}
      {fetchedAt ? ` · ${fetchedLabel(fetchedAt, now).toLowerCase()}` : ""}
    </NewerNotice>
  );
}

/** Read-only notice with the one way to do more: follow your repositories. */
function PreviewNotice() {
  const signedIn = useSignedIn();
  const [dismissed, setDismissed] = useState(
    () => saved(noticeKey) === "dismissed",
  );
  return (
    <Notice
      hidden={dismissed}
      icon={<EyeIcon aria-hidden="true" />}
      action={
        <>
          <LinkButton
            variant="outline-muted"
            size="sm"
            href={signedIn ? followPage : `/signin?next=${followPage}`}
            onClick={follow}
          >
            {signedIn ? "Open Settings" : "Sign in"}
          </LinkButton>
          <IconButton
            size="icon-sm"
            label="Dismiss preview notice"
            onClick={() => {
              save(noticeKey, "dismissed");
              setDismissed(true);
            }}
          >
            <XIcon aria-hidden="true" />
          </IconButton>
        </>
      }
    >
      {signedIn
        ? "Read-only preview of a public pull request. Follow your repositories in Settings to review their pull requests with your coding agent."
        : "Read-only preview of a public pull request. Sign in to follow your repositories and review their pull requests with your coding agent."}
    </Notice>
  );
}

type Failure = {
  title: string;
  description: string;
  /** Re-requests the preview; absent where retrying cannot help. */
  retry?: boolean;
  github?: boolean;
  /** Retry is offered once the fetch limit admits another fetch. */
  retryAt?: number;
};

/** Why the preview cannot show, or null while it can or may. */
function failure(preview: PublicPreview): Failure | null {
  const { state } = preview;
  if (state.status === "unavailable")
    return {
      title: "Pull request unavailable",
      description: notPublic,
      github: true,
    };
  if (state.status === "limited")
    return {
      title: "Preview limit reached",
      description: "",
      retryAt: state.retryAt,
    };
  if (state.status === "error")
    return {
      title: "Cannot load pull request",
      description: state.detail,
      retry: true,
    };
  const value = preview.preview;
  if (!value || value.contextId || value.status !== "failed" || !value.failure)
    return null;
  const name = value.fullName;
  const description = failureMessage(value.failure.code, name);
  if (value.failure.code === "not_found")
    return { title: "Pull request unavailable", description, github: true };
  if (value.failure.code === "too_large")
    return { title: "Repository too large", description, github: true };
  return { title: "Cannot fetch pull request", description, retry: true };
}

/** One-second clock for elapsed and remaining waits; idle otherwise. */
function useClock(active: boolean) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 1_000);
    return () => clearInterval(timer);
  }, [active]);
  return now;
}

/**
 * Until the snapshot opens: the diff skeleton with what the server is doing,
 * or why the preview cannot show and how to recover.
 */
function PublicStatus() {
  const { pull, preview } = usePreview();
  const { statusError, retry: retrySession, statusRef } = useSessionShell();
  const problem = failure(preview);
  const status = preview.preview?.status;
  const fetching = status === "queued" || status === "fetching";
  const now = useClock(
    (fetching && preview.viewedId === null) || problem?.retryAt !== undefined,
  );
  const name = pullRequestName(pull);
  if (statusError || problem) {
    const remaining =
      problem?.retryAt === undefined ? 0 : problem.retryAt - now;
    const retry = statusError
      ? retrySession
      : problem?.retry || (problem?.retryAt !== undefined && remaining <= 0)
        ? preview.retry
        : undefined;
    return (
      <main
        className="session-status"
        role="status"
        ref={statusRef}
        tabIndex={-1}
      >
        <EmptyState
          size="page"
          heading="h1"
          title={problem?.title ?? "Cannot load pull request"}
          description={
            statusError ||
            (problem?.retryAt !== undefined
              ? remaining > 0
                ? retryLabel(remaining / 1000)
                : "You can try again now."
              : problem?.description)
          }
          action={
            retry || problem?.github ? (
              <>
                {problem?.github && (
                  <LinkButton href={gitHubUrl(pull)} external>
                    Open on GitHub
                  </LinkButton>
                )}
                {retry && (
                  <Button type="button" variant="outline" onClick={retry}>
                    Retry
                  </Button>
                )}
              </>
            ) : undefined
          }
        />
      </main>
    );
  }
  const elapsed = now - preview.waitingSince;
  const title =
    preview.viewedId !== null ? (
      "Loading review"
    ) : status === "queued" ? (
      "Queued"
    ) : status === "fetching" ? (
      <>
        Fetching {name} from GitHub
        <span aria-hidden="true"> · {elapsedLabel(elapsed)}</span>
      </>
    ) : (
      "Loading pull request"
    );
  const detail =
    preview.viewedId !== null
      ? "Reading stored changes…"
      : fetching && elapsed >= slowFetchMilliseconds
        ? "First fetch of a large repository takes longer."
        : undefined;
  return (
    <main
      className="session-status is-loading"
      role="status"
      aria-busy="true"
      ref={statusRef}
      tabIndex={-1}
    >
      <WorkspaceLoading title={title} detail={detail} />
    </main>
  );
}
