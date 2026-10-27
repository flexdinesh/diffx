import type { ApiCommitSummary } from "@diffx/api";
import {
  ChevronDownIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
} from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { Button } from "@/components/ui/button";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxSearch,
  ComboboxTrigger,
} from "@/components/ui/combobox";
import { useDiffSource } from "./app-state.tsx";
import { commitMatches, shortOid } from "./commit-view.ts";
import { formatCount, pluralize } from "./format.ts";
import { relativeAge } from "./project-nav.ts";
import { useNow } from "./ProjectsNav.tsx";

/** Older/newer steps around a filterable list of the branch's commits. */
export function CommitStepper() {
  const { commit, changeCommit, stepCommit } = useDiffSource();
  const now = useNow();
  if (!commit) return null;
  const { commits, summary, omitted } = commit;
  const index = summary ? commits.indexOf(summary) : -1;
  const position =
    index >= 0 ? `${index + 1} of ${commits.length}` : "Unavailable";
  const label = summary
    ? `Commit ${position}: ${summary.subject}`
    : commit.status === "loading"
      ? "Loading commits"
      : `Commit ${shortOid(commit.oid)} unavailable`;
  return (
    <div className="comparison-subcontrol flex min-w-0 items-center gap-1">
      <IconButton
        label="Older commit"
        shortcut={{ alt: true, key: "[" }}
        variant="outline-muted"
        size="icon-xs"
        touch
        disabled={index <= 0}
        onClick={() => stepCommit(-1)}
      >
        <ChevronLeftIcon aria-hidden="true" />
      </IconButton>
      <Combobox
        items={commits}
        value={summary}
        autoHighlight
        isItemEqualToValue={(item, value) => item.oid === value.oid}
        itemToStringLabel={(item) => item.subject}
        filter={commitMatches}
        onValueChange={(next) => {
          if (next) changeCommit(next);
        }}
      >
        <ComboboxTrigger
          disabled={commits.length === 0}
          render={
            <Button
              type="button"
              variant="outline-muted"
              size="xs"
              touch
              className="min-w-0 shrink max-md:flex-1"
              aria-label={label}
            />
          }
        >
          {summary ? (
            <>
              <span className="font-mono text-muted">
                {shortOid(summary.oid)}
              </span>
              <span className="min-w-0 truncate">{summary.subject}</span>
              <span className="text-muted tabular-nums">
                {index + 1}/{commits.length}
              </span>
            </>
          ) : (
            <span className="truncate">{label}</span>
          )}
          <ChevronDownIcon aria-hidden="true" />
        </ComboboxTrigger>
        <ComboboxContent className="commit-menu">
          <ComboboxSearch
            label="Filter commits"
            placeholder="Filter by subject, SHA or author…"
          />
          <ComboboxEmpty>No matching commits</ComboboxEmpty>
          <ComboboxList aria-label="Commits, oldest first">
            {(item: ApiCommitSummary) => (
              <CommitItem key={item.oid} commit={item} now={now} />
            )}
          </ComboboxList>
          {omitted > 0 && (
            <p className="m-0 shrink-0 border-t border-separator px-3 py-2 text-xs text-muted">
              {pluralize(omitted, "older commit")} not captured
            </p>
          )}
        </ComboboxContent>
      </Combobox>
      <IconButton
        label="Newer commit"
        shortcut={{ alt: true, key: "]" }}
        variant="outline-muted"
        size="icon-xs"
        touch
        disabled={index < 0 || index >= commits.length - 1}
        onClick={() => stepCommit(1)}
      >
        <ChevronRightIcon aria-hidden="true" />
      </IconButton>
    </div>
  );
}

function CommitItem({
  commit,
  now,
}: {
  commit: ApiCommitSummary;
  now: number;
}) {
  return (
    <ComboboxItem value={commit}>
      <span className="shrink-0 font-mono text-xs text-muted">
        {shortOid(commit.oid)}
      </span>
      <span className="min-w-0 flex-1 truncate">{commit.subject}</span>
      {commit.merge ? (
        <span className="shrink-0 text-xs text-muted">Merge commit</span>
      ) : (
        <span className="shrink-0 font-mono text-xs tabular-nums">
          <span className="text-success">+{formatCount(commit.additions)}</span>{" "}
          <span className="text-error">−{formatCount(commit.deletions)}</span>
        </span>
      )}
      <span
        className="w-8 shrink-0 text-right font-mono text-xs text-muted tabular-nums"
        title={new Date(commit.authoredAt).toLocaleString()}
      >
        {relativeAge(commit.authoredAt, now)}
      </span>
    </ComboboxItem>
  );
}
