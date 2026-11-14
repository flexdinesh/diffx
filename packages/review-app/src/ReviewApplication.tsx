/// <reference types="vite/client" />
import type {} from "./webmcp-current.d.ts";
import { WorkerPoolContextProvider } from "@pierre/diffs/react";
import { useEffect, useState } from "react";
import { App } from "./App.tsx";
import { AppearanceProvider } from "./appearance-context.tsx";
import { SidebarProvider } from "./sidebar-context.tsx";
import { AppProvider } from "./app-state.tsx";
import { highlighterOptions, poolOptions } from "./diff-workers.ts";
import { SessionProvider } from "./session-context.tsx";
import { SessionShell } from "./SessionShell.tsx";
import "@diffx/ui/typography.css";
import "@diffx/ui/tokens.css";
import "./style.css";
import "./loading.css";
import "./review.css";
import "./account.css";
import "./landing.css";
import {
  IdentityProvider,
  IdentityStatus,
  navigate,
  useIdentity,
} from "./identity-context.tsx";
import {
  AccountSettings,
  InviteSignUp,
  ResetPassword,
  SignIn,
  SignUp,
  signupOpen,
} from "./account.tsx";
import { Landing } from "./Landing.tsx";
import { landingVisuals } from "./landing-visuals/index.tsx";
import { PublicPullRequest } from "./PublicPullRequest.tsx";
import { PublicReview } from "./PublicReview.tsx";
import {
  publicPullRequestLocation,
  pullRequestPath,
} from "./public-pull-request.ts";
import {
  useBrowserActivation,
  type ApplicationPlatform,
} from "./use-browser-activation.ts";
import { isSettingsPath, settingsSection } from "./settings-layout.tsx";
import {
  consentPath,
  consentReturn,
  isOAuthPath,
  OAuthConsent,
  OAuthRequestInvalid,
} from "./oauth-consent.tsx";
import { returnQuery } from "./public-pull-request.ts";
import { HistoryPage } from "./HistoryPage.tsx";
import { historyRoute } from "./history.ts";
import { TooltipProvider } from "@diffx/ui/components/ui/tooltip";

export interface ReviewApplicationProps {
  platform: ApplicationPlatform;
}

function ApplicationRoutes({ platform }: ReviewApplicationProps) {
  const { state } = useIdentity();
  const [path, setPath] = useState(window.location.pathname);
  useEffect(() => {
    const changed = () => setPath(window.location.pathname);
    window.addEventListener("popstate", changed);
    return () => window.removeEventListener("popstate", changed);
  }, []);
  const ready = state.status === "ready";
  const remote = ready && state.bootstrap.mode === "remote";
  const signedIn = ready && state.bootstrap.user !== null;
  // Public pull request previews answer GitHub's own pull request paths.
  const publicPreviews =
    ready &&
    state.bootstrap.mode === "remote" &&
    state.bootstrap.publicPullRequests;
  const preview = publicPreviews
    ? publicPullRequestLocation({
        pathname: path,
        search: window.location.search,
      })
    : null;
  const canonical = preview?.href ?? null;
  useEffect(() => {
    if (
      canonical === null ||
      canonical ===
        `${window.location.pathname}${window.location.search}${window.location.hash}`
    )
      return;
    window.history.replaceState(null, "", canonical);
    setPath(window.location.pathname);
  }, [canonical]);
  // History reads GitHub pull requests, so only GitHub servers offer it.
  const history =
    ready && state.bootstrap.mode === "remote" && state.bootstrap.githubEnabled;
  const userId = (ready && state.bootstrap.user?.id) || null;
  useBrowserActivation(platform, remote, userId);
  // Local mode has no account pages; signed-in users have no sign-in pages.
  const misplaced =
    ready &&
    ((!remote && (isSettingsPath(path) || isOAuthPath(path))) ||
      (!history && path === historyRoute) ||
      (signedIn &&
        (path === "/signin" ||
          path === "/signup" ||
          path === "/invite" ||
          path === "/reset-password")));
  useEffect(() => {
    if (!misplaced) return;
    window.history.replaceState(null, "", "/");
    setPath("/");
  }, [misplaced]);
  // Signed-out consent signs in first, then returns to the request.
  const consentSignIn =
    remote && !signedIn && path === consentPath
      ? consentReturn(window.location)
      : null;
  useEffect(() => {
    if (consentSignIn)
      navigate(`/signin${returnQuery(consentSignIn)}`, { replace: true });
  }, [consentSignIn]);
  if (state.status !== "ready") return <IdentityStatus />;
  if (preview)
    return (
      <PublicPullRequest
        key={pullRequestPath(preview.pull)}
        pull={preview.pull}
      >
        <PublicReview />
      </PublicPullRequest>
    );
  if (state.bootstrap.mode === "remote" && isOAuthPath(path)) {
    if (consentSignIn) return null;
    if (state.bootstrap.user && path === consentPath) return <OAuthConsent />;
    return <OAuthRequestInvalid />;
  }
  if (state.bootstrap.mode === "remote" && !state.bootstrap.user) {
    // Pages for a method the server turned off render sign-in.
    if (path === "/signup" && signupOpen(state.bootstrap)) return <SignUp />;
    if (path === "/invite") return <InviteSignUp />;
    if (path === "/reset-password") return <ResetPassword />;
    // Home is the landing page; sign-in lives at /signin, and home still
    // reports Google sign-in outcomes returned to it.
    if (
      path === "/" &&
      !new URLSearchParams(window.location.search).has("signin")
    )
      return (
        <Landing
          previews={state.bootstrap.publicPullRequests}
          visuals={landingVisuals}
        />
      );
    return <SignIn />;
  }
  if (isSettingsPath(path) && state.bootstrap.mode === "remote")
    return <AccountSettings section={settingsSection(path)} />;
  if (history && path === historyRoute) return <HistoryPage />;
  return (
    <SessionProvider key={state.bootstrap.user?.id ?? "local"}>
      <SessionShell>
        <WorkerPoolContextProvider
          poolOptions={poolOptions}
          highlighterOptions={highlighterOptions}
        >
          <AppProvider>
            <App />
          </AppProvider>
        </WorkerPoolContextProvider>
      </SessionShell>
    </SessionProvider>
  );
}

export function ReviewApplication({ platform }: ReviewApplicationProps) {
  return (
    <SidebarProvider>
      <AppearanceProvider>
        <IdentityProvider>
          <TooltipProvider>
            <ApplicationRoutes platform={platform} />
          </TooltipProvider>
        </IdentityProvider>
      </AppearanceProvider>
    </SidebarProvider>
  );
}
