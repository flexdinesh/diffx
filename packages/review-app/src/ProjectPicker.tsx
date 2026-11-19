import type { ApiContext } from "@diffx/api";
import {
  ArrowLeftIcon,
  ChevronRightIcon,
  CheckIcon,
  GitBranchIcon,
  GitPullRequestIcon,
  LinkIcon,
  SlidersHorizontalIcon,
  XIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { ContextIcon } from "@diffx/ui/components/context-icon";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { ListRow } from "@diffx/ui/components/list-row";
import { SearchField } from "@diffx/ui/components/search-field";
import { Shortcut } from "@diffx/ui/components/shortcut";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@diffx/ui/components/ui/dialog";
import { cn } from "@diffx/ui/lib/utils";
import { pluralize } from "@diffx/ui/lib/format";
import {
  contextChangeLabel,
  contextDetail,
  contextDiagnostics,
  contextIsLinkedWorktree,
  contextHasChanges,
  contextUnavailableReason,
  defaultPickerFilters,
  pickerEntryAvailable,
  pickerFilterOptions,
  pickerEntries,
  observationDetail,
} from "./project-picker.ts";
import {
  PickerFilterControls,
  pickerFiltersChanged,
  pickerFilterSummary,
} from "./PickerFilters.tsx";
import { pullRequestOf } from "./project-nav.ts";
import {
  pullRequestHead,
  pullRequestLink,
  pullRequestName,
  type PullRequestRef,
} from "./public-pull-request.ts";

/** Picker options beside its entries; context IDs never start with ":". */
const openPullRequestOption = ":open-pull-request";
const pullRequestOption = ":pull-request";

const groupLabel = "px-2 pt-2 pb-1 text-xs font-semibold text-secondary";

/**
 * Opening any public GitHub pull request by its link: `heads` are the bench
 * heads a followed repository's pull request may already have.
 */
export type PickerPullRequestLinks = {
  heads: readonly ApiContext[];
  open: (pull: PullRequestRef, head: ApiContext | undefined) => void;
};

function ChangeCount({ context }: { context: ApiContext }) {
  const title = "Changed files across staged, unstaged, and untracked changes";
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
  pullRequestLinks,
}: {
  contexts: ApiContext[];
  selectedId: string;
  select: (id: string) => void;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Null where this reader cannot open pull requests by link. */
  pullRequestLinks: PickerPullRequestLinks | null;
}) {
  const [query, setQuery] = useState("");
  const [repositoryId, setRepositoryId] = useState("");
  // "Open a pull request…" chosen: the field takes only a link.
  const [linkMode, setLinkMode] = useState(false);
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
  const link = pullRequestLinks ? pullRequestLink(query) : null;
  const linkSection =
    pullRequestLinks !== null &&
    (linkMode || (link !== null && link.kind !== "none"));
  const linkPull = linkSection && link?.kind === "pull" ? link.pull : null;
  const linkHead =
    linkPull && pullRequestLinks
      ? pullRequestHead(pullRequestLinks.heads, linkPull)
      : undefined;
  const linkTitle = linkHead ? pullRequestOf(linkHead)?.title : undefined;
  const showOpenAction =
    pullRequestLinks !== null && !linkSection && !repositoryId;
  const options = linkSection
    ? linkPull
      ? [pullRequestOption]
      : []
    : [
        ...selectable.map((entry) => entry.id),
        ...(showOpenAction ? [openPullRequestOption] : []),
      ];
  const activeId = linkSection
    ? options[0]
    : highlightedId === openPullRequestOption && showOpenAction
      ? openPullRequestOption
      : (active?.id ?? options[0]);
  const optionId = (id: string) => `${listId}-${id}`;
  const changeOpen = (next: boolean) => {
    if (!next) {
      setRepositoryId("");
      setLinkMode(false);
      setQuery("");
      setHighlightedId("");
    }
    onOpenChange(next);
  };
  const choose = (id: string) => {
    const entry = results.find((result) => result.id === id);
    if (!entry || !pickerEntryAvailable(entry)) return;
    if (entry.kind !== "context") {
      setRepositoryId(entry.context.repositoryId ?? "");
      setHighlightedId("");
      searchRef.current?.focus();
      return;
    }
    changeOpen(false);
    select(id);
  };
  const run = (id: string) => {
    if (id === openPullRequestOption) {
      setLinkMode(true);
      setQuery("");
      setHighlightedId("");
      searchRef.current?.focus();
    } else if (id === pullRequestOption) {
      if (!linkPull || !pullRequestLinks) return;
      changeOpen(false);
      pullRequestLinks.open(linkPull, linkHead);
    } else choose(id);
  };
  const back = () => {
    setRepositoryId("");
    if (linkMode) {
      setLinkMode(false);
      setQuery("");
    }
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
      if (contexts.length || pullRequestLinks) onOpenChange(true);
      if (open) searchRef.current?.focus();
    };
    document.addEventListener("keydown", shortcut);
    return () => document.removeEventListener("keydown", shortcut);
  }, [contexts.length, onOpenChange, open, pullRequestLinks]);

  const activeOptionId = activeId ? optionId(activeId) : undefined;
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
          Choose a repository, then a collected snapshot. Search branches,
          worktrees, hosts, and runs. Use arrow keys to navigate and Enter to
          choose.
          {pullRequestLinks &&
            " Or paste a GitHub pull request link to open that pull request."}
        </DialogDescription>
        <div>
          <SearchField
            ref={searchRef}
            className="m-2 w-auto"
            label="Search reviews"
            // A combobox, not a search box: no native clear beside Close.
            type="text"
            role="combobox"
            aria-autocomplete="list"
            aria-expanded={open}
            aria-controls={listId}
            aria-activedescendant={activeOptionId}
            placeholder={
              linkMode
                ? "https://github.com/owner/repo/pull/123"
                : repositoryId
                  ? `Search ${repository?.name ?? "observations"}…`
                  : pullRequestLinks
                    ? "Search repositories, or paste a pull request link…"
                    : "Search repositories…"
            }
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setHighlightedId("");
            }}
            onKeyDown={(event) => {
              if (event.nativeEvent.isComposing) return;
              const index = activeId ? options.indexOf(activeId) : -1;
              if (event.key === "ArrowDown" || event.key === "ArrowUp") {
                event.preventDefault();
                const next =
                  options[
                    Math.max(
                      0,
                      Math.min(
                        options.length - 1,
                        index + (event.key === "ArrowDown" ? 1 : -1),
                      ),
                    )
                  ];
                if (next) setHighlightedId(next);
              } else if (
                event.key === "ArrowLeft" &&
                (repositoryId || linkMode) &&
                !query
              ) {
                event.preventDefault();
                back();
              } else if (event.key === "Enter" && activeId) {
                event.preventDefault();
                run(activeId);
              }
            }}
            leading={
              repositoryId || linkMode ? (
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
                {!linkSection && (
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
          <div id={filtersId} hidden={!filtersOpen || linkSection}>
            <PickerFilterControls
              filters={filters}
              options={pickerFilterOptions(contexts)}
              onChange={(next) => {
                setFilters(next);
                setHighlightedId("");
              }}
            />
          </div>
          <div
            className="flex items-center gap-2 px-3 pb-2 text-xs text-muted"
            hidden={linkSection}
          >
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
        </div>
        <div
          id={listId}
          className="max-h-[calc(8*var(--space-12))] scroll-py-1 overflow-auto overscroll-contain px-1 pb-1"
          role="listbox"
          aria-label={
            linkSection
              ? "Pull request link"
              : repositoryId
                ? "Collected snapshots"
                : "Reviews"
          }
        >
          {linkSection && (
            <>
              <div className={groupLabel} role="presentation">
                Pull request link
              </div>
              {linkPull ? (
                <ListRow
                  type="button"
                  role="option"
                  variant="option"
                  tabIndex={-1}
                  id={optionId(pullRequestOption)}
                  aria-selected={activeId === pullRequestOption}
                  onClick={() => run(pullRequestOption)}
                >
                  <GitPullRequestIcon
                    className="size-icon"
                    aria-hidden="true"
                  />
                  <span className="min-w-0 flex-1">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="min-w-0 truncate font-semibold">
                        {pullRequestName(linkPull)}
                      </span>
                      <span className="shrink-0 font-mono text-sm text-secondary">
                        #{linkPull.number}
                      </span>
                    </span>
                    <span className="block truncate text-xs text-muted">
                      {linkHead
                        ? (linkTitle ?? "Opens the review on your bench")
                        : "Read-only preview of a public pull request"}
                    </span>
                  </span>
                  <Badge
                    variant="status"
                    tone={linkHead ? "success" : "neutral"}
                  >
                    {linkHead ? "On your bench" : "Preview"}
                  </Badge>
                </ListRow>
              ) : (
                <p
                  className={cn(
                    "m-0 flex items-center gap-2 px-2 py-3 text-sm",
                    link?.kind === "foreign" ? "text-error" : "text-muted",
                  )}
                >
                  {link?.kind === "foreign" ? (
                    <XIcon className="size-icon" aria-hidden="true" />
                  ) : (
                    <LinkIcon className="size-icon" aria-hidden="true" />
                  )}
                  {link?.kind === "foreign"
                    ? "Only github.com pull request links open here."
                    : "Paste a link like github.com/owner/repo/pull/123 or owner/repo#123."}
                </p>
              )}
            </>
          )}
          {!linkSection &&
            results.map((entry) => {
              const context = entry.context;
              const grouped = entry.kind !== "context";
              const available = pickerEntryAvailable(entry);
              return (
                <ListRow
                  type="button"
                  role="option"
                  variant="option"
                  tabIndex={-1}
                  id={optionId(entry.id)}
                  key={entry.id}
                  aria-selected={entry.id === activeId}
                  aria-disabled={!available}
                  disabled={!available}
                  title={`${contextDiagnostics(context)}${available ? "" : `\n${contextUnavailableReason()}`}`}
                  onClick={() => choose(entry.id)}
                  onMouseMove={() => {
                    if (available) setHighlightedId(entry.id);
                  }}
                >
                  <ContextIcon size="md" />
                  <span className="min-w-0 flex-1">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="max-w-1/2 min-w-0 shrink-0 truncate font-semibold">
                        {!grouped && context.observation
                          ? context.worktreeName || context.name
                          : context.name}
                      </span>
                      <span className="inline-flex min-w-0 items-center gap-1 text-sm text-secondary">
                        {!grouped && (
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
                    </span>
                    {!available && (
                      <span className="block text-xs text-muted">
                        {contextUnavailableReason()}
                      </span>
                    )}
                    <span className="block truncate font-mono text-xs font-normal text-muted">
                      {!grouped && contextIsLinkedWorktree(context) && (
                        <span className="font-sans">Worktree · </span>
                      )}
                      {grouped
                        ? context.observation?.remoteUrl ||
                          "Choose a collected snapshot"
                        : observationDetail(context)}
                    </span>
                  </span>
                  {!grouped && context.stale && (
                    <span className="shrink-0 text-xs text-warning">Stale</span>
                  )}
                  {!grouped && <ChangeCount context={context} />}
                  {grouped && (
                    <ChevronRightIcon
                      className="size-icon"
                      aria-hidden="true"
                    />
                  )}
                  {!grouped && context.id === selectedId && (
                    <span className="flex shrink-0 items-center gap-1 text-xs text-accent">
                      <CheckIcon className="size-icon" aria-hidden="true" />
                      <span className="sr-only">Current review</span>
                    </span>
                  )}
                </ListRow>
              );
            })}
          {!linkSection &&
            !results.length &&
            (contexts.length > 0 || !showOpenAction) && (
              <EmptyState
                className="my-6"
                heading="p"
                align="center"
                title={repositoryId ? "No snapshots found" : "No reviews found"}
                description="Try another search or clear filters."
              />
            )}
          {showOpenAction && (
            <div role="group" aria-labelledby={`${listId}-github`}>
              <div id={`${listId}-github`} className={groupLabel}>
                GitHub
              </div>
              <ListRow
                type="button"
                role="option"
                variant="option"
                tabIndex={-1}
                id={optionId(openPullRequestOption)}
                aria-selected={activeId === openPullRequestOption}
                onClick={() => run(openPullRequestOption)}
                onMouseMove={() => setHighlightedId(openPullRequestOption)}
              >
                <LinkIcon className="size-icon" aria-hidden="true" />
                <span className="min-w-0 flex-1">
                  <span className="block truncate font-semibold">
                    Open a pull request…
                  </span>
                  <span className="block truncate text-xs text-muted">
                    Paste any public github.com pull request link
                  </span>
                </span>
              </ListRow>
            </div>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-t border-separator bg-panel px-3 py-2 text-xs text-muted">
          <p className="m-0" role="status">
            {linkSection
              ? "Pull request link"
              : results.length
                ? pluralize(results.length, "result")
                : repositoryId
                  ? "No matching snapshots"
                  : "No matching reviews"}
          </p>
          <div className="flex flex-wrap items-center gap-3">
            {showOpenAction && (
              <span>
                <Shortcut keys={{ mod: true, key: "V" }} /> Paste a pull request
                link
              </span>
            )}
            <span>
              <Shortcut keys={{ key: "↑ ↓" }} /> Navigate
            </span>
            <span>
              <Shortcut keys={{ key: "Enter" }} />{" "}
              {linkSection ? "Open" : repositoryId ? "Review" : "Choose"}
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
