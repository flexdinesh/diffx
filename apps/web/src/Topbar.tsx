import type { ReactNode } from "react";
import { FolderSyncIcon, LoaderCircleIcon, SearchIcon } from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { AccountButton } from "./account.tsx";
import { ContextIdentity } from "./ContextIdentity.tsx";
import { HeaderBrand, ProjectsToggle } from "./ProjectsNav.tsx";
import { useCatalog } from "./session-context.tsx";
import { useShellPolicy } from "./use-shell-policy.ts";
import { useSidebarState } from "./sidebar-context.tsx";

export function CurrentContext() {
  const catalog = useCatalog();
  return catalog.current ? <ContextIdentity current={catalog.current} /> : null;
}

/**
 * Collects the local process's checkout, or every discovered checkout; shared
 * by the review and its loading shell.
 */
export function SyncButton({
  busy,
  onClick,
}: {
  busy: boolean;
  /** Omitted while the loading shell collects: the action is unavailable. */
  onClick?: () => void;
}) {
  const { collection } = useShellPolicy();
  return (
    <IconButton
      variant="shell"
      touch
      label={collection === "checkout" ? "Sync checkout" : "Sync all checkouts"}
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

/** Opens the review picker; Cmd/Ctrl+K opens it from anywhere. */
function SearchReviewsButton() {
  const { openPicker } = useCatalog();
  const { closeMobile } = useSidebarState();
  return (
    <IconButton
      id="reviews-search"
      variant="shell"
      touch
      label="Search reviews"
      shortcut={{ mod: true, key: "K" }}
      onClick={() => {
        closeMobile();
        openPicker();
      }}
    >
      <SearchIcon aria-hidden="true" />
    </IconButton>
  );
}

/**
 * The app header shared by the review and its loading/empty shell: brand,
 * current context, then trailing actions ending with account.
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
  /** Actions placed before account. */
  actions?: ReactNode;
}) {
  const { mobile } = useSidebarState();
  const { reviewNavigation } = useShellPolicy();
  const { reviewColumn } = useCatalog();
  return (
    <header className="topbar">
      {mobile && reviewColumn && <ProjectsToggle />}
      <HeaderBrand />
      {lead}
      <CurrentContext />
      {children}
      <div className="header-actions">
        {reviewNavigation && <SearchReviewsButton />}
        {actions}
        <AccountButton variant="shell" />
      </div>
    </header>
  );
}
