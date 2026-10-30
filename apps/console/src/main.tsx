import { StrictMode, useEffect } from "react";
import { createRoot } from "react-dom/client";
import { TooltipProvider } from "@diffx/ui/components/ui/tooltip";
import "@diffx/ui/typography.css";
import "@diffx/ui/tokens.css";
import "./console.css";
import { Accounts } from "./accounts.tsx";
import { Admin } from "./admin.tsx";
import { ResetPassword, Setup, SignIn } from "./auth.tsx";
import { Invites } from "./invites.tsx";
import {
  SessionProvider,
  SessionStatus,
  usePath,
  useSession,
} from "./session.tsx";
import { Shell, sections } from "./shell.tsx";

const root = document.getElementById("root");
if (!root) throw new Error("Missing React root");

const pages = { accounts: Accounts, invites: Invites, admin: Admin };

function Routes() {
  const { state } = useSession();
  const path = usePath();
  const admin = state.status === "ready" ? state.bootstrap.admin : null;
  const section = sections.find((value) => value.path === path);
  // Signed-in admins land on Accounts from any other path.
  useEffect(() => {
    if (admin && !section) window.history.replaceState(null, "", "/accounts");
  }, [admin, section]);
  if (state.status !== "ready") return <SessionStatus />;
  if (!admin) {
    if (path === "/setup" && state.bootstrap.setupRequired) return <Setup />;
    if (path === "/reset-password") return <ResetPassword />;
    return <SignIn />;
  }
  const current = section?.id ?? "accounts";
  const Content = pages[current];
  return (
    <Shell current={current}>
      <Content />
    </Shell>
  );
}

createRoot(root).render(
  <StrictMode>
    <TooltipProvider>
      <SessionProvider>
        <Routes />
      </SessionProvider>
    </TooltipProvider>
  </StrictMode>,
);
