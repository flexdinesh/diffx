import type { ApiContext } from "@diffx/api";
import {
  ArrowLeftIcon,
  ChevronRightIcon,
  CheckIcon,
  GitBranchIcon,
  SlidersHorizontalIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { EmptyState } from "@/components/empty-state";
import { IconButton } from "@/components/icon-button";
import { ListRow } from "@/components/list-row";
import { SearchField } from "@/components/search-field";
import { Shortcut } from "@/components/shortcut";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { Separator } from "@/components/ui/separator";
import { cn } from "@/lib/utils";
import { pluralize } from "./format.ts";
import {
  contextChangeLabel,
  contextDetail,
  contextDiagnostics,
  contextIsLinkedWorktree,
  contextHasChanges,
  contextIsPiped,
  contextTimestampLabel,
  contextUnavailableReason,
  defaultPickerFilters,
  pickerEntryAvailable,
  pickerFilterOptions,
  pickerEntries,
  pipedGroupId,
  observationDetail,
} from "./project-picker.ts";
import {
  PickerFilterControls,
  pickerFiltersChanged,
  pickerFilterSummary,
} from "./PickerFilters.tsx";

function ChangeCount({ context }: { context: ApiContext }) {
  const title = contextIsPiped(context)
    ? "Changed files in this snapshot"
    : "Changed files across staged, unstaged, and untracked changes";
  const count = contextHasChanges(context) ? context.changedFileCount : null;
  if (count !== null)
    return <CountBadge count={count} unit="changed file" title={title} />;
  return (
    <span
      className={cn(
        "shrink-0 text-xs whitespace-nowrap",
        context.availability === "unavailable" ? "text-warning" : "text-muted",
      )}
      title={title}
    >
      {contextChangeLabel(context)}
    </span>
  );
}

export function ProjectPicker({
  contexts,
  selectedId,
  select,
  open,
  onOpenChange,
}: {
  contexts: ApiContext[];
  selectedId: string;
  select: (id: string) => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [query, setQuery] = useState("");
  const [repositoryId, setRepositoryId] = useState("");
  const piped = repositoryId === pipedGroupId;
  const [highlightedId, setHighlightedId] = useState("");
  const [filters, setFilters] = useState(defaultPickerFilters);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);
  const listId = useId();
  const filtersId = useId();
  const results = pickerEntries(contexts, query, repositoryId, filters);
  const selectable = results.filter(pickerEntryAvailable);
  const repository = contexts.find(
    (context) => context.repositoryId === repositoryId,
  );
  const active =
    selectable.find((context) => context.id === highlightedId) ??
    (!query.trim()
      ? selectable.find((entry) =>
          entry.kind === "context"
            ? entry.context.id === selectedId
            : entry.contexts.some((context) => context.id === selectedId),
        )
      : undefined) ??
    selectable[0];
  const optionId = (id: string) => `${listId}-${id}`;
  const changeOpen = (next: boolean) => {
    if (!next) {
      setRepositoryId("");
      setQuery("");
      setHighlightedId("");
    }
    onOpenChange(next);
  };
  const choose = (id: string) => {
    const entry = results.find((result) => result.id === id);
    if (!entry || !pickerEntryAvailable(entry)) return;
    if (entry.kind !== "context") {
      setRepositoryId(
        entry.kind === "piped"
          ? pipedGroupId
          : (entry.context.repositoryId ?? ""),
      );
      if (entry.kind === "piped") setQuery("");
      setHighlightedId("");
      searchRef.current?.focus();
      return;
    }
    changeOpen(false);
    select(id);
  };
  const back = () => {
    setRepositoryId("");
    setHighlightedId("");
    searchRef.current?.focus();
  };

  useEffect(() => {
    const shortcut = (event: KeyboardEvent) => {
      if (
        event.defaultPrevented ||
        event.isComposing ||
        event.altKey ||
        !(event.metaKey || event.ctrlKey) ||
        event.key.toLowerCase() !== "k"
      )
        return;
      if (
        !open &&
        document.querySelector('[role="dialog"], [role="alertdialog"]')
      )
        return;
      event.preventDefault();
      if (contexts.length) onOpenChange(true);
      if (open) searchRef.current?.focus();
    };
    document.addEventListener("keydown", shortcut);
    return () => document.removeEventListener("keydown", shortcut);
  }, [contexts.length, onOpenChange, open]);

  const activeOptionId = active ? optionId(active.id) : undefined;
  useEffect(() => {
    if (open && activeOptionId)
      document
        .getElementById(activeOptionId)
        ?.scrollIntoView({ block: "nearest" });
  }, [open, activeOptionId]);

  return (
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent variant="command" initialFocus={searchRef}>
        <DialogTitle className="sr-only">Switch review</DialogTitle>
        <DialogDescription className="sr-only">
          Choose a repository or Piped, then a collected snapshot. Search
          branches, worktrees, hosts, and runs. Use arrow keys to navigate and
          Enter to choose.
        </DialogDescription>
        <div>
          <SearchField
            ref={searchRef}
            className="m-2 w-auto"
            label={piped ? "Search Piped" : "Search reviews"}
            // A combobox, not a search box: no native clear beside Close.
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-controls={listId}
            aria-activedescendant={activeOptionId}
            placeholder={
              piped
                ? "Search Piped…"
                : repositoryId
                  ? `Search ${repository?.name ?? "observations"}…`
                  : "Search repositories or Piped…"
            }
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setHighlightedId("");
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return;
              const index = selectable.findIndex(
                (context) => context.id === active?.id,
              );
              if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                const next =
                  selectable[
                    Math.max(
                      0,
                      Math.min(
                        selectable.length - 1,
                        index + (event.key === "ArrowDown" ? 1 : -1),
                      ),
                    )
                  ];
                if (next) setHighlightedId(next.id);
              } else if (event.key === "ArrowLeft" && repositoryId && !query) {
                event.preventDefault();
                back();
              } else if (event.key === "Enter" && active) {
                event.preventDefault();
                choose(active.id);
              }
            }}
            leading={
              repositoryId ? (
                <IconButton
                  label="Back to reviews"
                  size="icon-xs"
                  className="-ml-1"
                  onClick={back}
                >
                  <ArrowLeftIcon aria-hidden="true" />
                </IconButton>
              ) : undefined
            }
            actions={
              <>
                {!piped && (
                  <IconButton
                    label="Filters"
                    size="icon-xs"
                    aria-expanded={filtersOpen}
                    aria-controls={filtersId}
                    onClick={() => setFiltersOpen((value) => !value)}
                  >
                    <SlidersHorizontalIcon aria-hidden="true" />
                  </IconButton>
                )}
                <DialogClose
                  render={
                    <IconButton label="Close" size="icon-xs">
                      <XIcon aria-hidden="true" />
                    </IconButton>
                  }
                />
              </>
            }
          />
          <div id={filtersId} hidden={piped || !filtersOpen}>
            <PickerFilterControls
              filters={filters}
              options={pickerFilterOptions(contexts)}
              onChange={(next) => {
                setFilters(next);
                setHighlightedId("");
              }}
            />
          </div>
          {!piped && (
            <div className="flex items-center gap-2 px-3 pb-2 text-xs text-muted">
              <span
                className="min-w-0 flex-1 truncate"
                title={pickerFilterSummary(filters)}
              >
                {pickerFilterSummary(filters)}
              </span>
              {pickerFiltersChanged(filters) && (
                <Button
                  variant="ghost"
                  size="xs"
                  onClick={() => {
                    setFilters(defaultPickerFilters);
                    setHighlightedId("");
                  }}
                >
                  Clear filters
                </Button>
              )}
            </div>
          )}
          {piped && (
            <div className="px-3 pb-2 text-xs text-muted">
              Piped · Newest first
            </div>
          )}
        </div>
        <div
          id={listId}
          className="max-h-[calc(8*var(--space-12))] scroll-py-1 overflow-auto overscroll-contain px-1 pb-1"
          role="listbox"
          aria-label={
            piped ? "Piped" : repositoryId ? "Collected snapshots" : "Reviews"
          }
        >
          {results.map((entry) => {
            const context = entry.context;
            const grouped = entry.kind !== "context";
            const pipedContext = contextIsPiped(context);
            const available = pickerEntryAvailable(entry);
            return [
              entry.kind === "piped" && (
                <Separator
                  key={`${entry.id}-separator`}
                  aria-hidden="true"
                  className="mt-1"
                />
              ),
              <ListRow
                type="button"
                role="option"
                variant="option"
                tabIndex={-1}
                id={optionId(entry.id)}
                key={entry.id}
                aria-selected={entry.id === active?.id}
                aria-disabled={!available}
                disabled={!available}
                title={`${contextDiagnostics(context)}${available ? "" : `\n${contextUnavailableReason()}`}`}
                onClick={() => choose(entry.id)}
                onMouseMove={() => {
                  if (available) setHighlightedId(entry.id);
                }}
              >
                <ContextIcon piped={pipedContext} size="md" />
                <span className="min-w-0 flex-1">
                  <span className="flex min-w-0 items-center gap-2">
                    <span
                      className={cn(
                        "min-w-0 shrink-0 font-semibold",
                        !grouped && pipedContext
                          ? "max-w-full wrap-anywhere"
                          : "max-w-1/2 truncate",
                      )}
                    >
                      {entry.kind === "piped"
                        ? "Piped"
                        : pipedContext
                          ? contextTimestampLabel(context)
                          : grouped
                            ? context.name
                            : context.observation
                              ? context.worktreeName || context.name
                              : context.name}
                    </span>
                    {(!pipedContext || grouped) && (
                      <span className="inline-flex min-w-0 items-center gap-1 text-sm text-secondary">
                        {!grouped && !pipedContext && (
                          <GitBranchIcon
                            className="size-icon-sm"
                            aria-hidden="true"
                          />
                        )}
                        <span className="truncate">
                          {grouped
                            ? pluralize(entry.contexts.length, "snapshot")
                            : contextDetail(context)}
                        </span>
                      </span>
                    )}
                  </span>
                  {!available && (
                    <span className="block text-xs text-muted">
                      {contextUnavailableReason()}
                    </span>
                  )}
                  {(!pipedContext || grouped) && (
                    <span className="block truncate font-mono text-xs font-normal text-muted">
                      {!grouped && contextIsLinkedWorktree(context) && (
                        <span className="font-sans">Worktree · </span>
                      )}
                      {entry.kind === "piped"
                        ? "Choose a snapshot"
                        : grouped
                          ? context.observation?.remoteUrl ||
                            "Choose a collected snapshot"
                          : observationDetail(context)}
                    </span>
                  )}
                </span>
                {!grouped && !pipedContext && context.stale && (
                  <span className="shrink-0 text-xs text-warning">Stale</span>
                )}
                {!grouped && <ChangeCount context={context} />}
                {grouped && (
                  <ChevronRightIcon className="size-icon" aria-hidden="true" />
                )}
                {!grouped && context.id === selectedId && (
                  <span className="flex shrink-0 items-center gap-1 text-xs text-accent">
                    <CheckIcon className="size-icon" aria-hidden="true" />
                    <span className="sr-only">Current review</span>
                  </span>
                )}
              </ListRow>,
            ];
          })}
          {!results.length && (
            <EmptyState
              className="my-6"
              heading="p"
              align="center"
              title={repositoryId ? "No snapshots found" : "No reviews found"}
              description="Try another search or clear filters."
            />
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-separator bg-panel px-3 py-2 text-xs text-muted">
          <p className="m-0" role="status">
            {results.length
              ? pluralize(results.length, "result")
              : repositoryId
                ? "No matching snapshots"
                : "No matching reviews"}
          </p>
          <div className="flex flex-wrap items-center gap-3">
            <span>
              <Shortcut keys={{ key: "↑ ↓" }} /> Navigate
            </span>
            <span>
              <Shortcut keys={{ key: "Enter" }} />{" "}
              {repositoryId ? "Review" : "Choose"}
            </span>
            <span>
              <Shortcut keys={{ key: "Esc" }} /> Close
            </span>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
