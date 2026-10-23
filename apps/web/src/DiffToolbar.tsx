import {
  ChevronsDownUpIcon,
  ChevronsUpDownIcon,
  Settings2Icon,
} from "lucide-react";
import { IconButton } from "@/components/icon-button";
import { SegmentedControl } from "@/components/segmented-control";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useDiffCollapse, useNavigation } from "./app-state.tsx";
import { useAppearance } from "./appearance-context.tsx";
import {
  DIFF_THEMES,
  LINE_DIFF_TYPES,
  readDiffTheme,
  readLineDiffType,
} from "./display-options.ts";
import { ComparisonPicker } from "./ComparisonPicker.tsx";

const LAYOUTS = [
  { value: "split", label: "Split" },
  { value: "unified", label: "Unified" },
] as const;

export function DiffToolbar() {
  const {
    layout,
    setLayout,
    wrap,
    setWrap,
    diffTheme,
    setDiffTheme,
    lineDiffType,
    setLineDiffType,
  } = useAppearance();
  const { collapsed, setCollapsed } = useDiffCollapse();
  const { files } = useNavigation();
  const allCollapsed =
    files.length > 0 && files.every((file) => collapsed.has(file.path));
  return (
    <div className="toolbar [&_svg]:[stroke-width:var(--icon-stroke-chrome)]">
      <ComparisonPicker />
      <div className="min-w-0 flex-1" />
      <SegmentedControl
        label="Diff layout"
        className="max-md:hidden"
        touch
        options={LAYOUTS}
        value={layout}
        onValueChange={setLayout}
      />
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <IconButton
              id="view-options"
              label="View options"
              size="icon-sm"
              touch
            >
              <Settings2Icon aria-hidden="true" />
            </IconButton>
          }
        />
        <DropdownMenuContent
          align="end"
          className="min-w-56"
          aria-label="View options"
        >
          <DropdownMenuCheckboxItem checked={wrap} onCheckedChange={setWrap}>
            Wrap lines
          </DropdownMenuCheckboxItem>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>Layout</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              aria-label="Diff layout"
              value={layout}
              onValueChange={(value) => {
                if (value === "split" || value === "unified") setLayout(value);
              }}
            >
              {LAYOUTS.map((option) => (
                <DropdownMenuRadioItem key={option.value} value={option.value}>
                  {option.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>Inline changes</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              aria-label="Inline change detail"
              value={lineDiffType}
              onValueChange={(value) =>
                setLineDiffType(readLineDiffType(value))
              }
            >
              {LINE_DIFF_TYPES.map((option) => (
                <DropdownMenuRadioItem key={option.value} value={option.value}>
                  {option.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuLabel>Code theme</DropdownMenuLabel>
            <DropdownMenuRadioGroup
              aria-label="Code theme"
              value={diffTheme}
              onValueChange={(value) => setDiffTheme(readDiffTheme(value))}
            >
              {DIFF_THEMES.map((option) => (
                <DropdownMenuRadioItem key={option.value} value={option.value}>
                  {option.label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
        </DropdownMenuContent>
      </DropdownMenu>
      <IconButton
        id="toggle-all-files"
        label={allCollapsed ? "Expand all files" : "Collapse all files"}
        size="icon-sm"
        touch
        disabled={files.length === 0}
        onClick={() =>
          setCollapsed(
            allCollapsed ? new Set() : new Set(files.map((file) => file.path)),
          )
        }
      >
        {allCollapsed ? (
          <ChevronsUpDownIcon aria-hidden="true" />
        ) : (
          <ChevronsDownUpIcon aria-hidden="true" />
        )}
      </IconButton>
    </div>
  );
}
