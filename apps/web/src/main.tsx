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
  SignIn,
} from "./account.tsx";
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
  // Local mode has no account pages; signed-in users have no sign-in pages.
  const misplaced =
    ready &&
    ((!remote && path === "/settings") ||
      (signedIn && (path === "/setup" || path === "/admin/login")));
  useEffect(() => {
    if (!misplaced) return;
    window.history.replaceState(null, "", "/");
    setPath("/");
  }, [misplaced]);
  if (state.status !== "ready") return <IdentityStatus />;
  if (state.bootstrap.mode === "remote" && !state.bootstrap.user) {
    if (path === "/setup") return <AdminSetup />;
    if (path === "/admin/login") return <AdminSignIn />;
    return <SignIn />;
  }
  if (path === "/settings" && state.bootstrap.mode === "remote")
    return <AccountSettings />;
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
