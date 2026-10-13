import { WorkerPoolContextProvider } from "@pierre/diffs/react";
import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.tsx";
import { AppearanceProvider } from "./appearance-context.tsx";
import { ProjectsNavProvider } from "./projects-nav-context.tsx";
import { SidebarProvider } from "./sidebar-context.tsx";
import { AppProvider } from "./app-state.tsx";
import { highlighterOptions, poolOptions } from "./diff-workers.ts";
import { SessionProvider } from "./session-context.tsx";
import "@fontsource-variable/jetbrains-mono/wght.css";
import "./typography.css";
import "./tokens.css";
import "./style.css";
import "./review.css";
import "./account.css";
import {
  IdentityProvider,
  IdentityStatus,
  useIdentity,
} from "./identity-context.tsx";
import { SignIn, AccountSettings } from "./account.tsx";

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
  const localSettings =
    state.status === "ready" &&
    state.bootstrap.mode === "local" &&
    path === "/settings";
  useEffect(() => {
    if (!localSettings) return;
    window.history.replaceState(null, "", "/");
    setPath("/");
  }, [localSettings]);
  if (state.status !== "ready") return <IdentityStatus />;
  if (state.bootstrap.mode === "remote" && !state.bootstrap.user)
    return <SignIn />;
  if (path === "/settings" && state.bootstrap.mode === "remote")
    return <AccountSettings />;
  return (
    <ProjectsNavProvider key={state.bootstrap.user?.id ?? "local"}>
      <SessionProvider>
        <WorkerPoolContextProvider
          poolOptions={poolOptions}
          highlighterOptions={highlighterOptions}
        >
          <AppProvider>
            <App />
          </AppProvider>
        </WorkerPoolContextProvider>
      </SessionProvider>
    </ProjectsNavProvider>
  );
}

createRoot(root).render(
  <StrictMode>
    <SidebarProvider>
      <AppearanceProvider>
        <IdentityProvider>
          <ApplicationRoutes />
        </IdentityProvider>
      </AppearanceProvider>
    </SidebarProvider>
  </StrictMode>,
);
