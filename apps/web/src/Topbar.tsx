import type { ReactNode } from "react";
import { FolderSyncIcon, LoaderCircleIcon } from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { AccountButton } from "./account.tsx";
import { ContextIdentity } from "./ContextIdentity.tsx";
import {
  HeaderBrand,
  NewChangesButton,
  ProjectsToggle,
} from "./ProjectsNav.tsx";
import { useCatalog } from "./session-context.tsx";
import { ThemeButton } from "./ThemeButton.tsx";
import { useSidebarState } from "./sidebar-context.tsx";

export function CurrentContext() {
  const catalog = useCatalog();
  return catalog.current ? <ContextIdentity current={catalog.current} /> : null;
}

/** Collects every local checkout; shared by the review and its loading shell. */
export function SyncButton({
  busy,
  onClick,
}: {
  busy: boolean;
  /** Omitted while the loading shell collects: the action is unavailable. */
  onClick?: () => void;
}) {
  return (
    <IconButton
      variant="shell"
      touch
      label="Sync all checkouts"
      shortcut={{ alt: true, key: "R" }}
      aria-busy={busy}
      disabled={!onClick}
      onClick={onClick}
    >
      {busy ? (
        <LoaderCircleIcon
          className="motion-safe:animate-spinner"
          aria-hidden="true"
        />
      ) : (
        <FolderSyncIcon aria-hidden="true" />
      )}
    </IconButton>
  );
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
  /** Controls placed after the brand, e.g. the file sidebar toggle. */
  lead?: ReactNode;
  /** Content after the current context, e.g. the review heading. */
  children?: ReactNode;
  /** Actions placed before account and theme. */
  actions?: ReactNode;
}) {
  const { mobile } = useSidebarState();
  return (
    <header className="topbar">
      {mobile && <ProjectsToggle />}
      <HeaderBrand />
      {lead}
      <CurrentContext />
      {children}
      <div className="header-actions">
        <NewChangesButton />
        {actions}
        <AccountButton variant="shell" />
        <ThemeButton />
      </div>
    </header>
  );
}
