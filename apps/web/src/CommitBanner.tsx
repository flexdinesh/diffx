import { GitCommitHorizontalIcon, GitMergeIcon } from "lucide-react";
import { useId } from "react";
import { useSavedOpen } from "@/components/collapsible-section";
import { DisclosureIcon } from "@/components/disclosure";
import { IconButton } from "@/components/icon-button";
import { Badge } from "@/components/ui/badge";
import { useDiffSource } from "./app-state.tsx";
import { shortOid } from "./commit-view.ts";
import { relativeAge } from "./project-nav.ts";
import { useNow } from "./ProjectsNav.tsx";

/**
 * The viewed commit above its diff: SHA, subject, author and age, with the
 * rest of the message collapsed by default. Commit views are read-only.
 */
export function CommitBanner() {
  const { commit } = useDiffSource();
  const [open, setOpen] = useSavedOpen("commit-message", false);
  const messageId = useId();
  const now = useNow();
  const summary = commit?.summary;
  if (!summary) return null;
  const body = summary.body.trim();
  const Icon = summary.merge ? GitMergeIcon : GitCommitHorizontalIcon;
  return (
    <section
      className="shrink-0 border-b border-border bg-panel text-sm"
      aria-label={`Commit ${shortOid(summary.oid)}`}
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 px-(--diff-gutter) py-1">
        <Icon className="size-icon-sm text-muted" aria-hidden="true" />
        <h2 className="m-0 min-w-0 flex-1 truncate text-sm font-semibold">
          <span
            className="font-mono font-normal text-muted"
            title={summary.oid}
          >
            {shortOid(summary.oid)}
          </span>{" "}
          {summary.subject}
        </h2>
        {summary.merge && (
          <Badge variant="status" tone="neutral">
            Merge commit
          </Badge>
        )}
        <span className="text-secondary">
          <span className="sr-only">Author </span>
          {summary.authorName}
        </span>
        <time
          className="font-mono text-xs text-muted tabular-nums"
          dateTime={new Date(summary.authoredAt).toISOString()}
          title={new Date(summary.authoredAt).toLocaleString()}
        >
          {relativeAge(summary.authoredAt, now)}
        </time>
        {body && (
          <IconButton
            size="icon-sm"
            label={open ? "Hide message" : "Show message"}
            aria-expanded={open}
            aria-controls={messageId}
            onClick={() => setOpen((value) => !value)}
          >
            <DisclosureIcon open={open} />
          </IconButton>
        )}
      </div>
      {body && (
        <div
          id={messageId}
          hidden={!open}
          className="commit-message px-(--diff-gutter) pt-1 pb-3"
        >
          {/* Messages are plain text; keep authored line breaks. */}
          <pre className="m-0 max-w-(--reading-width) font-sans text-sm break-words whitespace-pre-wrap text-secondary">
            {body}
          </pre>
        </div>
      )}
    </section>
  );
}
