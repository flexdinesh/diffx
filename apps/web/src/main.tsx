import { WorkerPoolContextProvider } from "@pierre/diffs/react";
import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import { AppearanceProvider } from "./appearance-context.tsx";
import { SidebarProvider } from "./sidebar-context.tsx";
import { StreamNavProvider } from "./stream-nav-context.tsx";
import { AppProvider } from "./app-state.tsx";
import { highlighterOptions, poolOptions } from "./diff-workers.ts";
import { SessionProvider } from "./session-context.tsx";
import { SessionShell } from "./SessionShell.tsx";
import "@fontsource-variable/jetbrains-mono/wght.css";
import "./typography.css";
import "./tokens.css";
import "./style.css";
import "./loading.css";
import "./review.css";
import "./account.css";
import {
  IdentityProvider,
  IdentityStatus,
  useIdentity,
} from "./identity-context.tsx";
import {
  AccountSettings,
  AdminSetup,
  AdminSignIn,
  InviteSignUp,
  SignIn,
} from "./account.tsx";
import { AdminPage } from "./admin.tsx";
import { startPwa } from "./pwa.ts";
import { syncPushDevice } from "./push.ts";
import { TooltipProvider } from "@/components/ui/tooltip";

const root = document.getElementById("root");
if (!root) throw new Error("Missing React root");
function ApplicationRoutes() {
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
  const admin = signedIn && state.bootstrap.user?.admin === true;
  const userId = (ready && state.bootstrap.user?.id) || null;
  // Only the remote server is an installable app with push notifications.
  useEffect(() => {
    if (remote) startPwa();
  }, [remote]);
  useEffect(() => {
    if (remote && userId) void syncPushDevice().catch(() => {});
  }, [remote, userId]);
  // Local mode has no account pages; signed-in users have no sign-in pages;
  // only the admin has the admin page.
  const misplaced =
    ready &&
    ((!remote && (path === "/settings" || path === "/admin")) ||
      (signedIn &&
        (path === "/setup" ||
          path === "/admin/login" ||
          path === "/invite" ||
          (path === "/admin" && !admin))));
  useEffect(() => {
    if (!misplaced) return;
    window.history.replaceState(null, "", "/");
    setPath("/");
  }, [misplaced]);
  if (state.status !== "ready") return <IdentityStatus />;
  if (state.bootstrap.mode === "remote" && !state.bootstrap.user) {
    if (path === "/setup") return <AdminSetup />;
    if (path === "/admin/login") return <AdminSignIn />;
    if (path === "/invite") return <InviteSignUp />;
    return <SignIn />;
  }
  if (path === "/settings" && state.bootstrap.mode === "remote")
    return <AccountSettings />;
  if (path === "/admin" && admin) return <AdminPage />;
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

createRoot(root).render(
  <StrictMode>
    <SidebarProvider>
      <StreamNavProvider>
        <AppearanceProvider>
          <IdentityProvider>
            <TooltipProvider>
              <ApplicationRoutes />
            </TooltipProvider>
          </IdentityProvider>
        </AppearanceProvider>
      </StreamNavProvider>
    </SidebarProvider>
  </StrictMode>,
);
