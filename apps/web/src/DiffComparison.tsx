import { InfoIcon, XIcon } from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { KeyValue, KeyValueList } from "@/components/key-value-list";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverClose,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import { useDiffSource } from "./app-state.tsx";
import { diffComparison } from "./diff-comparison.ts";
import { useSession } from "./session-context.tsx";

export function DiffComparison() {
  const session = useSession();
  const { mode, repository, scopeAvailable } = useDiffSource();
  if (!scopeAvailable) return null;
  const comparison = diffComparison(
    session,
    mode,
    repository?.head,
    repository?.comparisonKind,
  );
  return (
    <Popover key={mode}>
      <PopoverTrigger
        aria-label={`Diff comparison: ${comparison.summary}`}
        render={
          <Button
            variant="ghost"
            size="inline"
            className="min-w-0 shrink justify-start"
          />
        }
      >
        <span className="truncate max-md:hidden">{comparison.summary}</span>
        <span className="hidden max-md:inline">Comparison</span>
        <InfoIcon aria-hidden="true" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-[min(30rem,calc(100vw-2*var(--space-4)))]"
      >
        <PopoverHeader>
          <div className="flex items-center justify-between gap-3">
            <PopoverTitle>Diff comparison</PopoverTitle>
            <PopoverClose
              render={
                <IconButton size="icon-xs" label="Close diff comparison">
                  <XIcon aria-hidden="true" />
                </IconButton>
              }
            />
          </div>
          <PopoverDescription>{comparison.description}</PopoverDescription>
        </PopoverHeader>
        {comparison.details.length > 0 && (
          <KeyValueList>
            {comparison.details.map(({ label, value }) => (
              <KeyValue key={label} label={label} font="mono">
                {value}
              </KeyValue>
            ))}
          </KeyValueList>
        )}
      </PopoverContent>
    </Popover>
  );
}
