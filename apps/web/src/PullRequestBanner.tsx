import type { ApiPullRequest } from "@diffx/api";
import {
  ExternalLinkIcon,
  GitMergeIcon,
  GitPullRequestClosedIcon,
  GitPullRequestDraftIcon,
  GitPullRequestIcon,
  type LucideIcon,
} from "lucide-react";
import { useId, type ReactNode } from "react";
import { useSavedOpen } from "@diffx/ui/components/collapsible-section";
import { DisclosureIcon } from "@diffx/ui/components/disclosure";
import { IconButton } from "@diffx/ui/components/icon-button";
import { LinkButton } from "@diffx/ui/components/link-button";
import { MarkdownText } from "@diffx/ui/components/markdown";
import { Badge } from "@diffx/ui/components/ui/badge";
import { pullRequestOf } from "./project-nav.ts";
import { useSession } from "./session-context.tsx";

type Status = {
  label: string;
  tone: "accent" | "error" | "neutral" | "success";
  Icon: LucideIcon;
};

/** State as GitHub names it; draft applies only while open. */
export function pullRequestStatus(pull: ApiPullRequest): Status {
  if (pull.state === "merged")
    return { label: "Merged", tone: "accent", Icon: GitMergeIcon };
  if (pull.state === "closed")
    return { label: "Closed", tone: "error", Icon: GitPullRequestClosedIcon };
  if (pull.draft)
    return { label: "Draft", tone: "neutral", Icon: GitPullRequestDraftIcon };
  return { label: "Open", tone: "success", Icon: GitPullRequestIcon };
}

/**
 * The open review's GitHub pull request above the diff: number, title, state,
 * author and branches, with the description collapsed by default.
 */
export function PullRequestBanner() {
  const session = useSession();
  const pull = pullRequestOf(session);
  return pull ? <PullRequestSummary pull={pull} /> : null;
}

/**
 * The pull request banner for a known pull request. `actions` follow "Open on
 * GitHub", e.g. a public preview's refresh.
 */
export function PullRequestSummary({
  pull,
  actions,
}: {
  pull: ApiPullRequest;
  actions?: ReactNode;
}) {
  const [open, setOpen] = useSavedOpen("pull-request-description", false);
  const descriptionId = useId();
  const status = pullRequestStatus(pull);
  const body = pull.body.trim();
  return (
    <section
      className="shrink-0 border-b border-border bg-panel text-sm"
      aria-label={`Pull request #${pull.number}`}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 px-(--diff-gutter) py-1">
        <status.Icon className="size-icon-sm text-muted" aria-hidden="true" />
        <h2 className="m-0 min-w-0 flex-1 truncate text-sm font-semibold">
          <span className="font-mono font-normal text-muted">
            #{pull.number}
          </span>{" "}
          {pull.title}
        </h2>
        <Badge variant="status" tone={status.tone}>
          {status.label}
        </Badge>
        <span className="text-secondary">
          <span className="sr-only">Author </span>@{pull.author.login}
        </span>
        <span
          className="min-w-0 truncate font-mono text-xs text-muted"
          title={`${pull.headRepository || pull.fullName}:${pull.headRef} into ${pull.baseRef}`}
        >
          <span className="sr-only">Merging </span>
          {pull.headRef}
          <span aria-hidden="true"> → </span>
          <span className="sr-only"> into </span>
          {pull.baseRef}
        </span>
        <LinkButton variant="outline-muted" size="sm" href={pull.url} external>
          Open on GitHub
          <ExternalLinkIcon className="size-icon-sm" aria-hidden="true" />
        </LinkButton>
        {actions}
        {body && (
          <IconButton
            size="icon-sm"
            label={open ? "Hide description" : "Show description"}
            aria-expanded={open}
            aria-controls={descriptionId}
            onClick={() => setOpen((value) => !value)}
          >
            <DisclosureIcon open={open} />
          </IconButton>
        )}
      </div>
      {body && (
        <div
          id={descriptionId}
          hidden={!open}
          className="pull-request-description px-(--diff-gutter) pt-1 pb-3"
        >
          <MarkdownText className="max-w-(--reading-width)">
            {body}
          </MarkdownText>
        </div>
      )}
    </section>
  );
}
