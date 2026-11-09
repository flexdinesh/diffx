import type { ApiContext } from "@diffx/api";
import {
  GitBranchIcon,
  GitMergeIcon,
  GitPullRequestClosedIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
  HistoryIcon,
} from "lucide-react";
import { useEffect, useState, type MouseEvent, type ReactNode } from "react";
import { BrandMark } from "@diffx/ui/components/brand-mark";
import { Notice } from "@diffx/ui/components/notice";
import { StatusDot } from "@diffx/ui/components/status-dot";
import { Button } from "@diffx/ui/components/ui/button";
import { Separator } from "@diffx/ui/components/ui/separator";
import { contextChangeLabel } from "./project-picker.ts";
import { contextActivity } from "./context-activity.ts";
import {
  newerHead,
  pullRequestOf,
  relativeAge,
  streamTitle,
} from "./project-nav.ts";
import { useCatalog, useSession } from "./session-context.tsx";

// Stream marks shared by the Review bench, header comboboxes and notices.

export function HeaderBrand() {
  return (
    <>
      <a className="brand" href="/" aria-label="diffx home">
        <BrandMark />
      </a>
      <Separator
        variant="chrome"
        orientation="vertical"
        className="header-divider"
      />
    </>
  );
}

export function useNow() {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

/** Opens a stream in place unless the click asks the browser for a new tab. */
export function streamClick(event: MouseEvent<HTMLElement>, open: () => void) {
  if (event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
    return;
  event.preventDefault();
  open();
}

/** Pull requests carry their GitHub state; other streams show a branch. */
export function StreamIcon({
  context,
  muted = false,
}: {
  context: ApiContext;
  muted?: boolean;
}) {
  const pull = pullRequestOf(context);
  const className = muted ? "text-muted" : undefined;
  if (!pull) return <GitBranchIcon className={className} aria-hidden="true" />;
  const [Icon, label] =
    pull.state === "merged"
      ? [GitMergeIcon, "Merged pull request"]
      : pull.state === "closed"
        ? [GitPullRequestClosedIcon, "Closed pull request"]
        : pull.draft
          ? [GitPullRequestDraftIcon, "Draft pull request"]
          : [GitPullRequestIcon, "Pull request"];
  return <Icon className={className} role="img" aria-label={label} />;
}

export function NewMark() {
  return <StatusDot tone="accent" size="sm" label="New changes" />;
}

/** Announces a newer head for the viewed stream without replacing the review. */
export function NewerSnapshotNotice() {
  const session = useSession();
  const { heads, openStream } = useCatalog();
  const newer = newerHead(session, heads.contexts);
  const now = useNow();
  return (
    <NewerNotice
      onView={newer && (() => openStream(newer, { restoreFocus: false }))}
    >
      {newer &&
        `Newer snapshot of ${streamTitle(newer)} · ${contextChangeLabel(newer)} · ${relativeAge(contextActivity(newer), now)}`}
    </NewerNotice>
  );
}

/**
 * Announces a newer snapshot of the open review without swapping it; stays
 * mounted (hidden) so its arrival is announced. `onView` opens the latest.
 */
export function NewerNotice({
  onView,
  children,
}: {
  onView: (() => void) | undefined;
  children: ReactNode;
}) {
  return (
    <Notice
      hidden={!onView}
      icon={onView && <HistoryIcon aria-hidden="true" />}
      action={
        onView && (
          <Button
            type="button"
            variant="outline-muted"
            size="sm"
            onClick={onView}
          >
            View latest
          </Button>
        )
      }
    >
      {onView && children}
    </Notice>
  );
}
