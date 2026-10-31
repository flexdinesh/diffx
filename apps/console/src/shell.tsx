import type { ReactNode } from "react";
import { LinkButton } from "@diffx/ui/components/link-button";
import { navigate, useAdmin } from "./session.tsx";

export type Section = "accounts" | "invites" | "settings" | "admin";

export const sections: { id: Section; path: string; label: string }[] = [
  { id: "accounts", path: "/accounts", label: "Accounts" },
  { id: "invites", path: "/invites", label: "Invites" },
  { id: "settings", path: "/settings", label: "Settings" },
  { id: "admin", path: "/admin", label: "Admin" },
];

/** The signed-in console: brand, section navigation, then one page. */
export function Shell({
  current,
  children,
}: {
  current: Section;
  children: ReactNode;
}) {
  const admin = useAdmin();
  return (
    <div className="console-shell">
      <header className="console-topbar">
        <a
          className="brand"
          href="/accounts"
          onClick={(event) => {
            event.preventDefault();
            navigate("/accounts");
          }}
        >
          diffx <span className="brand-product">Console</span>
        </a>
        <nav className="console-nav" aria-label="Console">
          {sections.map((section) => (
            <LinkButton
              key={section.id}
              variant="rail"
              size="sm"
              touch
              href={section.path}
              aria-current={section.id === current ? "page" : undefined}
              onClick={(event) => {
                event.preventDefault();
                navigate(section.path);
              }}
            >
              {section.label}
            </LinkButton>
          ))}
        </nav>
        <span className="console-identity">{admin.username}</span>
      </header>
      <main className="console-page">{children}</main>
    </div>
  );
}
