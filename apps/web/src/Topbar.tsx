import type { ReactNode } from "react";
import { AccountButton } from "./account.tsx";
import { ContextIdentity } from "./ContextIdentity.tsx";
import {
  HeaderBrand,
  NewChangesButton,
  ProjectsToggle,
} from "./ProjectsNav.tsx";
import { useCatalog } from "./session-context.tsx";
import { ThemeButton } from "./ThemeButton.tsx";
import { useCompactChrome } from "./use-compact-chrome.ts";

export function CurrentContext() {
  const catalog = useCatalog();
  return catalog.current ? <ContextIdentity current={catalog.current} /> : null;
}

/**
 * The app header shared by the review and its loading/empty shell: brand,
 * current context, then trailing actions ending with account and theme.
 */
export function Topbar({
  lead,
  children,
  actions,
}: {
  /** Compact-only controls placed after the brand, e.g. the file sidebar toggle. */
  lead?: ReactNode;
  /** Content after the current context, e.g. the review heading. */
  children?: ReactNode;
  /** Actions placed before account and theme. */
  actions?: ReactNode;
}) {
  const compact = useCompactChrome();
  return (
    <header className="topbar">
      {compact && <ProjectsToggle />}
      <HeaderBrand />
      {compact && lead}
      <CurrentContext />
      {children}
      <div className="header-actions">
        <NewChangesButton />
        {actions}
        {compact && <AccountButton />}
        <ThemeButton />
      </div>
    </header>
  );
}
