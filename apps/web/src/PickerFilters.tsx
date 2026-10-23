import { ChevronDownIcon } from "lucide-react";
import { EmptyState } from "@/components/empty-state";
import { SegmentedControl } from "@/components/segmented-control";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  defaultPickerFilters,
  type PickerFilters,
  type pickerFilterOptions,
} from "./project-picker.ts";

const freshnessLabels = {
  all: "All snapshots",
  latest: "Latest snapshots",
  stale: "Stale snapshots",
};

const freshnessOptions = [
  { value: "all", label: "All" },
  { value: "latest", label: "Latest" },
  { value: "stale", label: "Stale" },
] satisfies { value: PickerFilters["freshness"]; label: string }[];

export function pickerFilterSummary(filters: PickerFilters) {
  return [
    freshnessLabels[filters.freshness],
    filters.includeAll ? "All statuses" : "With changes",
    ...filters.hosts.map((host) => `Host: ${host}`),
    ...filters.branches.map((branch) => `Branch: ${branch}`),
    ...filters.worktrees.map((worktree) => `Worktree: ${worktree}`),
  ].join(" · ");
}

export function pickerFiltersChanged(filters: PickerFilters) {
  return (
    filters.freshness !== defaultPickerFilters.freshness ||
    filters.includeAll !== defaultPickerFilters.includeAll ||
    filters.hosts.length > 0 ||
    filters.branches.length > 0 ||
    filters.worktrees.length > 0
  );
}

function filterTrigger(label: string) {
  return (
    <Button variant="outline-muted" size="sm">
      {label}
      <ChevronDownIcon aria-hidden="true" />
    </Button>
  );
}

/** A facet menu of checkable values; the trigger counts the selection. */
export function MultiFilter({
  label,
  options,
  selected,
  onChange,
}: {
  label: string;
  options: string[];
  selected: string[];
  onChange: (values: string[]) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={label}
        render={filterTrigger(
          `${label}${selected.length ? ` (${selected.length})` : ""}`,
        )}
      />
      <DropdownMenuContent className="max-w-[min(var(--reading-width),calc(100vw-2*var(--page-gutter)))] wrap-anywhere">
        {!options.length && (
          <EmptyState description={`No recorded ${label.toLowerCase()}`} />
        )}
        {options.map((value) => (
          <DropdownMenuCheckboxItem
            key={value}
            checked={selected.includes(value)}
            closeOnClick={false}
            onCheckedChange={(checked) =>
              onChange(
                checked
                  ? [...selected, value]
                  : selected.filter((item) => item !== value),
              )
            }
          >
            {value}
          </DropdownMenuCheckboxItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function PickerFilterControls({
  filters,
  options,
  onChange,
}: {
  filters: PickerFilters;
  options: ReturnType<typeof pickerFilterOptions>;
  onChange: (filters: PickerFilters) => void;
}) {
  return (
    <div
      className="flex flex-wrap gap-2 px-3 pb-2"
      role="group"
      aria-label="Repository filters"
    >
      <div className="flex w-full items-center justify-between gap-2">
        <SegmentedControl
          label="Snapshot freshness"
          size="sm"
          touch
          options={freshnessOptions}
          value={filters.freshness}
          onValueChange={(freshness) => onChange({ ...filters, freshness })}
        />
        <label
          className="inline-flex min-h-control cursor-pointer items-center gap-1 text-sm text-secondary"
          title="Include unavailable snapshots and snapshots with no changes or unknown status"
        >
          <Checkbox
            checked={filters.includeAll}
            onCheckedChange={(includeAll) =>
              onChange({ ...filters, includeAll })
            }
          />
          All
        </label>
      </div>
      <MultiFilter
        label="Host"
        options={options.hosts}
        selected={filters.hosts}
        onChange={(hosts) => onChange({ ...filters, hosts })}
      />
      <MultiFilter
        label="Branch"
        options={options.branches}
        selected={filters.branches}
        onChange={(branches) => onChange({ ...filters, branches })}
      />
      <MultiFilter
        label="Worktree"
        options={options.worktrees}
        selected={filters.worktrees}
        onChange={(worktrees) => onChange({ ...filters, worktrees })}
      />
    </div>
  );
}
