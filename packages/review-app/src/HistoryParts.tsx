import {
  GitMergeIcon,
  GitPullRequestClosedIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
  type LucideIcon,
} from "lucide-react";
import type { ReactNode } from "react";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { LinkButton } from "@diffx/ui/components/link-button";
import { Button } from "@diffx/ui/components/ui/button";
import {
  clearFilters,
  type HistoryData,
  type HistoryPullState,
  type HistoryQuery,
} from "./history.ts";
import { navigate } from "./identity-context.tsx";
import { settingsPath } from "./settings-layout.tsx";
import { streamClick } from "./StreamParts.tsx";

export const pullStateIcons: Record<HistoryPullState, LucideIcon> = {
  open: GitPullRequestIcon,
  draft: GitPullRequestDraftIcon,
  closed: GitPullRequestClosedIcon,
  merged: GitMergeIcon,
};

const pullStateNames: Record<HistoryPullState, string> = {
  open: "Open pull request",
  draft: "Draft pull request",
  closed: "Closed pull request",
  merged: "Merged pull request",
};

/** A pull request's state now, as a muted icon. */
export function PullIcon({ state }: { state: HistoryPullState }) {
  const Icon = pullStateIcons[state];
  return (
    <Icon
      className="text-muted"
      role="img"
      aria-label={pullStateNames[state]}
    />
  );
}

function SettingsAction({ label }: { label: string }) {
  const href = settingsPath("github");
  return (
    <LinkButton
      variant="outline"
      href={href}
      onClick={(event) => streamClick(event, () => navigate(href))}
    >
      {label}
    </LinkButton>
  );
}

/**
 * Why History shows nothing: no GitHub connection, no followed repositories,
 * filters matching nothing, or an empty window.
 */
export function HistoryEmpty({
  data,
  query,
  filtered = false,
  phrase,
  onChange,
}: {
  data: Pick<HistoryData, "connected" | "following" | "login">;
  query: HistoryQuery;
  filtered?: boolean;
  phrase?: string;
  onChange: (query: HistoryQuery) => void;
}) {
  let title: string;
  let description: string;
  let action: ReactNode = null;
  if (!data.connected) {
    title = "Connect GitHub to see your history";
    description =
      "History lists the pull requests you open and merge in repositories you follow.";
    action = <SettingsAction label="Connect GitHub" />;
  } else if (!data.following) {
    title = "Follow repositories to see your history";
    description = `History lists the pull requests @${data.login ?? "you"} opens and merges in repositories you follow.`;
    action = <SettingsAction label="Follow repositories" />;
  } else if (filtered) {
    title = "No matching pull requests";
    description = "No pull request in this range matches the filters.";
    action = (
      <Button
        type="button"
        variant="outline"
        onClick={() => onChange(clearFilters(query))}
      >
        Clear filters
      </Button>
    );
  } else {
    title = `No pull requests ${phrase ?? ""}`.trim();
    description =
      "Pull requests you open or merge in followed repositories appear here.";
    if (query.range !== "30d")
      action = (
        <Button
          type="button"
          variant="outline"
          onClick={() => onChange({ ...query, range: "30d", end: "", day: "" })}
        >
          Show 30 days
        </Button>
      );
  }
  return (
    <EmptyState
      size="page"
      heading="h2"
      role="status"
      title={title}
      description={description}
      action={action}
    />
  );
}
