import { ChevronDownIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { SegmentedControl } from "@/components/segmented-control";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useDiffSource } from "./app-state.tsx";
import { comparisonKind, comparisonLabel } from "./diff-mode.ts";
import { useSession } from "./session-context.tsx";

export function ComparisonPicker() {
  const session = useSession();
  const { mode, changeMode, scopeAvailable } = useDiffSource();
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
  const selected = scopeAvailable
    ? comparisonLabel(comparisonKind(session, mode))
    : "Unavailable comparison";
  const localSelected =
    scopeAvailable && local.some(({ scope }) => scope === mode);
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
    </>
  );
}
