import { ChevronDownIcon } from "lucide-react";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { Button } from "@diffx/ui/components/ui/button";
import { SegmentedControl } from "@diffx/ui/components/segmented-control";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { useDiffSource } from "./app-state.tsx";
import { CommitStepper } from "./CommitStepper.tsx";
import { comparisonKind, comparisonLabel } from "./diff-mode.ts";
import { pluralize } from "@diffx/ui/lib/format";
import { useSession } from "./session-context.tsx";

export function ComparisonPicker() {
  const session = useSession();
  const { mode, changeMode, scopeAvailable, commit, openCommits } =
    useDiffSource();
  const commitCount = session.commits?.count ?? 0;
  const scopes = session.capabilities.diff.scopes.values;
  const entries = scopes.map((scope) => ({
    scope,
    kind: comparisonKind(session, scope),
  }));
  const local = ["uncommitted", "staged", "unstaged"].flatMap((kind) =>
    entries.filter((entry) => entry.kind === kind),
  );
  const choices = [
    local.find(({ kind }) => kind === "uncommitted") ?? local[0],
    entries.find(({ kind }) => kind === "committed"),
    entries.find(({ kind }) => kind === "combined"),
    entries.find(({ kind }) => kind === "patch"),
  ].flatMap((entry) => (entry ? [entry] : []));
  const commitsLabel = comparisonLabel("commit");
  const selected = commit
    ? commitsLabel
    : scopeAvailable
      ? comparisonLabel(comparisonKind(session, mode))
      : "Unavailable comparison";
  const localSelected =
    !commit && scopeAvailable && local.some(({ scope }) => scope === mode);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="outline-muted"
              size="xs"
              touch
              aria-label={`Comparison: ${selected}`}
            >
              {selected}
              <ChevronDownIcon aria-hidden="true" />
            </Button>
          }
        />
        <DropdownMenuContent align="start" aria-label="Comparison">
          <DropdownMenuRadioGroup
            value={selected}
            onValueChange={(value) => {
              if (value === commitsLabel) {
                if (!commit) openCommits();
                return;
              }
              const next = choices.find(
                ({ kind }) => comparisonLabel(kind) === value,
              );
              if (next) changeMode(next.scope);
            }}
          >
            {choices.map(({ scope, kind }) => (
              <DropdownMenuRadioItem
                closeOnClick
                key={scope}
                value={comparisonLabel(kind)}
              >
                {comparisonLabel(kind)}
              </DropdownMenuRadioItem>
            ))}
            {(commitCount > 0 || commit) && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuRadioItem closeOnClick value={commitsLabel}>
                  {commitsLabel}
                  <CountBadge
                    className="ml-auto"
                    count={commitCount}
                    label={pluralize(commitCount, "commit")}
                  />
                </DropdownMenuRadioItem>
              </>
            )}
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      {localSelected && local.length > 1 && (
        <div className="comparison-subcontrol">
          <SegmentedControl
            label="Uncommitted scope"
            touch
            flush
            options={local.map(({ scope, kind }) => ({
              value: scope,
              label:
                kind === "uncommitted"
                  ? "All"
                  : kind === "staged"
                    ? "Staged"
                    : "Unstaged",
              ...(kind === "uncommitted" && { ariaLabel: "All changes" }),
            }))}
            value={mode}
            onValueChange={changeMode}
          />
        </div>
      )}
      <CommitStepper />
    </>
  );
}
