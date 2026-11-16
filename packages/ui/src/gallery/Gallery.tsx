import {
  CopyIcon,
  FileDiffIcon,
  HistoryIcon,
  MessageSquareWarningIcon,
  PencilIcon,
  RefreshCwIcon,
  Trash2Icon,
} from "lucide-react";
import { useState, type ReactNode } from "react";
import { LinkButton } from "@diffx/ui/components/link-button";
import { MarkdownText } from "@diffx/ui/components/markdown";
import { Badge } from "@diffx/ui/components/ui/badge";
import { Button } from "@diffx/ui/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@diffx/ui/components/ui/card";
import { Checkbox } from "@diffx/ui/components/ui/checkbox";
import {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxSearch,
  ComboboxTrigger,
} from "@diffx/ui/components/ui/combobox";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@diffx/ui/components/ui/dropdown-menu";
import { Input } from "@diffx/ui/components/ui/input";
import { Kbd, KbdGroup } from "@diffx/ui/components/ui/kbd";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverSection,
  PopoverTitle,
  PopoverTrigger,
} from "@diffx/ui/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@diffx/ui/components/ui/select";
import { Separator } from "@diffx/ui/components/ui/separator";
import { Skeleton } from "@diffx/ui/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@diffx/ui/components/ui/tabs";
import { Textarea } from "@diffx/ui/components/ui/textarea";
import { Toggle } from "@diffx/ui/components/ui/toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@diffx/ui/components/ui/tooltip";
import { ChoiceList, ChoiceListItem } from "@diffx/ui/components/choice-list";
import {
  BarChart,
  BarChartLegend,
  type BarChartBin,
  type BarChartSeries,
} from "@diffx/ui/components/bar-chart";
import { BrandMark } from "@diffx/ui/components/brand-mark";
import { CollapsibleSection } from "@diffx/ui/components/collapsible-section";
import { ConfirmDialog } from "@diffx/ui/components/confirm-dialog";
import { ContextIcon } from "@diffx/ui/components/context-icon";
import { CountBadge } from "@diffx/ui/components/count-badge";
import { DisclosureIcon } from "@diffx/ui/components/disclosure";
import { EmptyState } from "@diffx/ui/components/empty-state";
import { ErrorState } from "@diffx/ui/components/error-state";
import { IconButton } from "@diffx/ui/components/icon-button";
import { KeyValue, KeyValueList } from "@diffx/ui/components/key-value-list";
import { ListRow } from "@diffx/ui/components/list-row";
import { Notice } from "@diffx/ui/components/notice";
import { PanelHeader } from "@diffx/ui/components/panel-header";
import { ReadoutStrip } from "@diffx/ui/components/readout-strip";
import { SearchField } from "@diffx/ui/components/search-field";
import { SegmentedControl } from "@diffx/ui/components/segmented-control";
import { Shortcut } from "@diffx/ui/components/shortcut";
import { Sparkline } from "@diffx/ui/components/sparkline";
import { SplitButton } from "@diffx/ui/components/split-button";
import { StatusDot } from "@diffx/ui/components/status-dot";
import { ThemeSwatch } from "@diffx/ui/components/theme-swatch";
import { formatShortcut } from "@diffx/ui/lib/format";

const colors = [
  ["bg", "bg-bg"],
  ["panel", "bg-panel"],
  ["shell", "bg-shell"],
  ["raised", "bg-raised"],
  ["hover", "bg-hover"],
  ["count", "bg-count"],
  ["selected", "bg-selected"],
  ["accent", "bg-accent"],
  ["success", "bg-success"],
  ["success-bg", "bg-success-bg"],
  ["warning", "bg-warning"],
  ["warning-bg", "bg-warning-bg"],
  ["error", "bg-error"],
  ["error-bg", "bg-error-bg"],
  ["fg", "bg-fg"],
  ["secondary", "bg-secondary"],
  ["muted", "bg-muted"],
  ["border", "bg-border"],
  ["separator", "bg-separator"],
  ["control", "bg-control"],
] as const;

const typeScale = [
  ["xs", "text-xs"],
  ["sm", "text-sm"],
  ["base", "text-base"],
  ["md", "text-md"],
  ["xl", "text-xl"],
  ["code", "text-code font-mono"],
] as const;

const buttonVariants = [
  "default",
  "outline",
  "outline-muted",
  "outline-check",
  "ghost",
  "shell",
  "rail",
  "subtle",
  "destructive",
  "destructive-solid",
  "link",
] as const;

const buttonSizes = ["xs", "sm", "default", "lg"] as const;
const iconSizes = ["icon-xs", "icon-sm", "icon", "icon-lg"] as const;
const tones = ["neutral", "accent", "success", "warning", "error"] as const;
const skeletonShapes = [
  "line",
  "code",
  "label",
  "icon",
  "control",
  "field",
] as const;
const layouts = [
  { value: "split", label: "Split" },
  { value: "unified", label: "Unified" },
] as const;

const galleryMarkdown = [
  "## Summary",
  "",
  "Adds **search** with `useSearch()` and [docs](#markdown).",
  "",
  "- [x] Index files",
  "- [ ] Rank results",
  "",
  "> Follow-up: cache queries.",
  "",
  "```ts",
  "const hits = search(query);",
  "```",
  "",
  "| Case | Result |",
  "| --- | --- |",
  "| empty | none |",
].join("\n");

const activitySeries: BarChartSeries[] = [
  { name: "merged", mark: "solid" },
  { name: "opened", mark: "hollow" },
];
const weekdays = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
const months = ["Oct", "Nov"];
// Deterministic sample counts: plausible weekday peaks, quiet weekends.
function sampleCount(seed: number, scale: number): number {
  return Math.floor(((Math.sin(seed * 12.9898) * 43758.5453) % 1) ** 2 * scale);
}
// Days ending Sun 16 Nov 2026, oldest first.
function dayBins(count: number, kept: number): BarChartBin[] {
  return Array.from({ length: count }, (_, index) => {
    const ago = count - 1 - index;
    const date = new Date(2026, 10, 16 - ago);
    const weekday = weekdays[date.getDay()] ?? "";
    const month = months[date.getMonth() - 9] ?? "";
    const weekend = date.getDay() === 0 || date.getDay() === 6;
    const scale = weekend ? 3 : 12;
    const tick =
      count <= 7
        ? `${weekday} ${date.getDate()}`
        : date.getDay() === 1
          ? `${date.getDate()} ${month}`
          : "";
    return {
      id: `day-${ago}`,
      label: `${weekday} ${date.getDate()} ${month}`,
      ...(tick ? { tick } : {}),
      values: [sampleCount(index + 1, scale), sampleCount(index + 41, scale)],
      unavailable: ago >= kept,
    };
  });
}
const weekBins = dayBins(7, 7);
const monthBins = dayBins(30, 24);
// Today's hours; it is 14:20, so later hours have not happened yet.
const hourBins: BarChartBin[] = Array.from({ length: 24 }, (_, hour) => {
  const time = `${String(hour).padStart(2, "0")}:00`;
  return {
    id: `hour-${hour}`,
    label: `Mon 16 Nov, ${time}`,
    ...(hour % 6 === 0 ? { tick: time } : {}),
    values:
      hour > 14 || hour < 8
        ? [0, 0]
        : [sampleCount(hour + 7, 4), sampleCount(hour + 70, 5)],
    disabled: hour > 14,
  };
});
const yearBins: BarChartBin[] = Array.from({ length: 52 }, (_, week) => ({
  id: `week-${week}`,
  label: `Week ${week + 1}`,
  ...(week % 13 === 0 ? { tick: `W${week + 1}` } : {}),
  values: [sampleCount(week + 3, 40), sampleCount(week + 90, 50)],
}));

function Section({
  id,
  title,
  children,
}: {
  id: string;
  title: string;
  children: ReactNode;
}) {
  return (
    <section
      data-gallery={id}
      aria-label={title}
      className="flex flex-col gap-3"
    >
      <h2 className="m-0 text-sm font-semibold tracking-label text-secondary">
        {title}
      </h2>
      {children}
    </section>
  );
}

function Row({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-center gap-2">{children}</div>;
}

function Components() {
  const [layout, setLayout] = useState<"split" | "unified">("split");
  return (
    <div className="flex flex-col gap-8">
      <Section id="tokens" title="Tokens">
        <div className="grid grid-cols-10 gap-2">
          {colors.map(([name, className]) => (
            <div
              key={name}
              className="flex flex-col gap-1 text-xs text-secondary"
            >
              <span className={`${className} h-8 border border-border`} />
              {name}
            </div>
          ))}
        </div>
        <Row>
          {typeScale.map(([name, className]) => (
            <span key={name} className={className}>
              {name} Aa
            </span>
          ))}
        </Row>
      </Section>

      <Section id="button" title="Button">
        {buttonVariants.map((variant) => (
          <Row key={variant}>
            {buttonSizes.map((size) => (
              <Button key={size} variant={variant} size={size}>
                <RefreshCwIcon aria-hidden="true" />
                {variant} {size}
              </Button>
            ))}
            <Button variant={variant} disabled>
              Disabled
            </Button>
          </Row>
        ))}
        <Row>
          {iconSizes.map((size) => (
            <IconButton
              key={size}
              label={`Refresh ${size}`}
              size={size}
              variant="outline"
            >
              <RefreshCwIcon aria-hidden="true" />
            </IconButton>
          ))}
          <Button variant="outline" touch>
            Touch
          </Button>
          <IconButton label="Touch icon" touch>
            <PencilIcon aria-hidden="true" />
          </IconButton>
          <Button variant="outline-check" size="sm" aria-pressed>
            Reviewed
          </Button>
          <IconButton label="Changes" variant="rail" aria-current="page">
            <FileDiffIcon aria-hidden="true" />
          </IconButton>
          <LinkButton variant="outline-muted" size="sm" href="#button">
            Link button
          </LinkButton>
          <span className="text-xs text-secondary">
            Status row ·{" "}
            <Button variant="link" size="inline">
              inline trigger
            </Button>
          </span>
        </Row>
        <Row>
          <SplitButton
            menuLabel="Copy options"
            menu={<DropdownMenuItem>Copy all</DropdownMenuItem>}
          >
            <CopyIcon aria-hidden="true" />
            Copy review
          </SplitButton>
          <SplitButton
            variant="destructive"
            size="xs"
            menuLabel="Delete options"
            menu={<DropdownMenuItem>Delete resolved</DropdownMenuItem>}
          >
            <Trash2Icon aria-hidden="true" />
            Delete
          </SplitButton>
        </Row>
      </Section>

      <Section id="toggle" title="Toggle and segmented control">
        <Row>
          <Toggle aria-label="Hide reviewed" defaultPressed>
            <FileDiffIcon aria-hidden="true" />
          </Toggle>
          <Toggle aria-label="Wrap" variant="outline" size="sm">
            Wrap
          </Toggle>
          <Toggle aria-label="Wrap compact" size="xs">
            Wrap
          </Toggle>
          <SegmentedControl
            label="Diff layout"
            options={layouts}
            value={layout}
            onValueChange={setLayout}
          />
          <SegmentedControl
            label="Diff layout small"
            size="sm"
            options={layouts}
            value={layout}
            onValueChange={setLayout}
          />
          <SegmentedControl
            label="Diff layout flush"
            flush
            touch
            options={layouts}
            value={layout}
            onValueChange={setLayout}
          />
        </Row>
      </Section>

      <Section id="tabs" title="Tabs and panel header">
        <div className="flex w-full max-w-(--drawer-width) flex-col">
          <Tabs defaultValue="files">
            <PanelHeader
              actions={
                <IconButton
                  label="Collapse all folders"
                  variant="shell"
                  size="icon-sm"
                >
                  <DisclosureIcon open />
                </IconButton>
              }
            >
              <TabsList aria-label="Sidebar view">
                <TabsTrigger value="files">
                  Changes <CountBadge count={12} />
                </TabsTrigger>
                <TabsTrigger value="comments">
                  Review <CountBadge count={0} />
                </TabsTrigger>
              </TabsList>
            </PanelHeader>
          </Tabs>
          <PanelHeader
            title="Repos"
            actions={
              <IconButton label="Search reviews" size="icon-sm">
                <HistoryIcon aria-hidden="true" />
              </IconButton>
            }
          />
        </div>
      </Section>

      <Section id="badge" title="Badge and status">
        <Row>
          <CountBadge count={3} />
          <CountBadge count={1280} />
          {tones.map((tone) => (
            <Badge key={tone} variant="status" tone={tone}>
              {tone}
            </Badge>
          ))}
          <StatusDot tone="success" label="Live" />
          <StatusDot tone="accent" label="New changes" />
          <StatusDot tone="warning" size="md" fill="ring" label="Unstaged" />
          <StatusDot
            tone="warning"
            size="md"
            fill="half"
            label="Partly staged"
          />
          <StatusDot tone="success" size="md" label="Staged" />
          <ContextIcon piped={false} />
          <ContextIcon piped size="md" />
          <ContextIcon piped={false} tone="muted" />
          <BrandMark />
        </Row>
      </Section>

      <Section id="fields" title="Fields">
        <div className="grid max-w-(--reading-width) grid-cols-2 gap-3">
          <Input aria-label="Name" placeholder="Name" />
          <Input aria-label="Invalid" aria-invalid defaultValue="Invalid" />
          <SearchField
            label="Filter files"
            placeholder="Filter files…"
            shortcut={{ alt: true, key: "/" }}
          />
          <SearchField
            label="Search reviews"
            placeholder="Search repositories or Piped…"
            actions={
              <IconButton label="Clear search" size="icon-xs">
                <PencilIcon aria-hidden="true" />
              </IconButton>
            }
          />
          <Select
            defaultValue="30"
            items={{ "30": "30 days", "90": "90 days" }}
          >
            <SelectTrigger aria-label="Expiry">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="30">30 days</SelectItem>
              <SelectItem value="90">90 days</SelectItem>
            </SelectContent>
          </Select>
          <Row>
            <Checkbox aria-label="Unchecked" />
            <Checkbox aria-label="Checked" defaultChecked />
            <Kbd>Esc</Kbd>
            <KbdGroup>
              <Shortcut keys={{ mod: true, key: "K" }} />
              <Shortcut keys={{ alt: true, key: "R" }} />
            </KbdGroup>
          </Row>
          <Textarea
            aria-label="Comment"
            placeholder="Leave a review comment…"
          />
          <Textarea
            aria-label="Copy text"
            variant="code"
            defaultValue={"src/value.ts:3\nCheck this badge tone."}
          />
        </div>
      </Section>

      <Section id="card" title="Card">
        <div className="grid max-w-(--reading-width) gap-3">
          {(["inline", "panel"] as const).map((density) => (
            <Card key={density} density={density}>
              <CardHeader>
                <CardTitle>Review comment · line 3</CardTitle>
                <CardAction>
                  <Badge variant="status">Open</Badge>
                </CardAction>
              </CardHeader>
              <CardContent>Check this badge tone ({density}).</CardContent>
              <CardFooter>
                <Button variant="ghost" size="sm">
                  Edit
                </Button>
                <Button variant="outline" size="sm">
                  Resolve
                </Button>
              </CardFooter>
            </Card>
          ))}
          <Card>
            <CardHeader>
              <CardTitle className="gap-2">
                <span className="shrink-0">New comment</span>
                <span className="min-w-0 truncate font-mono text-xs">
                  src/value.ts:3
                </span>
              </CardTitle>
            </CardHeader>
            <CardContent>
              <Textarea
                aria-label="Draft"
                placeholder="Leave a review comment…"
                variant="comment"
              />
            </CardContent>
            <CardFooter>
              <span className="mr-auto text-xs text-muted">
                {formatShortcut({ mod: true, key: "Enter" })} to comment
              </span>
              <Button variant="ghost" size="xs">
                Cancel
              </Button>
              <Button size="xs">Comment</Button>
            </CardFooter>
          </Card>
        </div>
      </Section>

      <Section id="rows" title="List rows and sections">
        <div className="flex w-full max-w-(--drawer-width) flex-col">
          <ListRow aria-expanded>
            <DisclosureIcon open />
            <ContextIcon piped={false} />
            diffx
          </ListRow>
          <ListRow depth={1} aria-current="page">
            main · 12 files
          </ListRow>
          <ListRow font="mono" depth={1} aria-expanded={false}>
            <DisclosureIcon open={false} />
            src
          </ListRow>
          <ListRow font="mono" depth={2} aria-current="true">
            value.ts
          </ListRow>
          <ListRow font="mono" depth={2}>
            format.ts
          </ListRow>
          <ListRow aria-selected="true" role="option">
            Selected option
          </ListRow>
          <ListRow variant="option" aria-selected="true" role="option">
            <ContextIcon piped={false} size="md" />
            Picker option
          </ListRow>
          <ListRow disabled>Unavailable</ListRow>
          <CollapsibleSection title="Summary" storageKey="gallery-summary">
            <div className="p-3">
              <KeyValueList>
                <KeyValue label="Files changed">12</KeyValue>
                <KeyValue label="Additions">+43</KeyValue>
                <KeyValue label="Deletions">−16</KeyValue>
              </KeyValueList>
            </div>
          </CollapsibleSection>
        </div>
      </Section>

      <Section id="choices" title="Choice list and theme swatches">
        <ChoiceList
          aria-label="Theme"
          defaultValue="dark"
          className="w-full max-w-(--drawer-width)"
        >
          <ChoiceListItem value="system">
            <ThemeSwatch palettes={["diffx-light", "diffx-dark"]} />
            System
          </ChoiceListItem>
          <ChoiceListItem value="dark">
            <ThemeSwatch palettes={["diffx-dark"]} />
            Dark
          </ChoiceListItem>
          <ChoiceListItem value="tokyo-night">
            <ThemeSwatch palettes={["tokyo-night"]} />
            Tokyo Night
          </ChoiceListItem>
          <ChoiceListItem value="flexoki-light">
            <ThemeSwatch palettes={["flexoki-light"]} />
            Flexoki Light
          </ChoiceListItem>
        </ChoiceList>
      </Section>

      <Section id="states" title="States">
        <Notice
          icon={<HistoryIcon aria-hidden="true" />}
          action={
            <Button variant="outline-muted" size="sm">
              View latest
            </Button>
          }
        >
          Newer snapshot of main · 12 files · now
        </Notice>
        <Notice
          tone="warning"
          icon={<MessageSquareWarningIcon aria-hidden="true" />}
        >
          2 open comments are outdated by code changes
        </Notice>
        <Notice tone="neutral">Reading review context…</Notice>
        <EmptyState
          size="page"
          title="No changes to review"
          description="Run diffx to review a checkout, or diffx sync to publish remotely."
          action={<Button variant="outline">Retry</Button>}
        />
        <EmptyState
          title="No current changes"
          description="Agent hooks and diffx sync publish changed checkouts here."
        />
        <ErrorState message="Unable to load repos." onRetry={() => undefined} />
        <Row>
          {skeletonShapes.map((shape) => (
            <Skeleton key={shape} shape={shape} className="w-16" />
          ))}
          <Separator orientation="vertical" />
          <Separator variant="chrome" orientation="vertical" />
        </Row>
      </Section>

      <Charts />

      <Section id="markdown" title="Markdown">
        <MarkdownText className="max-w-(--reading-width)">
          {galleryMarkdown}
        </MarkdownText>
      </Section>
    </div>
  );
}

function Charts() {
  const [day, setDay] = useState("day-2");
  const [hour, setHour] = useState("hour-10");
  return (
    <Section id="charts" title="Readouts and charts">
      <ReadoutStrip
        label="Activity summary, 10 – 16 Nov"
        cells={[
          { value: "47", label: "merged" },
          { value: "63", label: "opened" },
          { value: "12", label: "open now" },
          { value: "8", label: "repositories" },
        ]}
      />
      <div className="flex max-w-(--reading-width) flex-col gap-2">
        <BarChart
          label="Activity, 10 – 16 Nov"
          series={activitySeries}
          bins={weekBins}
          selected={day}
          onActivate={setDay}
        />
        <BarChartLegend series={activitySeries} />
      </div>
      <BarChart
        label="Activity, 18 Oct – 16 Nov"
        series={activitySeries}
        bins={monthBins}
        className="max-w-(--reading-width)"
      />
      <BarChart
        label="Activity, Mon 16 Nov"
        series={activitySeries}
        bins={hourBins}
        selected={hour}
        marker={{ index: 14, fraction: 1 / 3, label: "Now, 14:20" }}
        onActivate={setHour}
        className="max-w-(--reading-width)"
      />
      <div className="flex w-full max-w-90 flex-col gap-2 border border-border p-3">
        <BarChart
          label="Activity, 2026 by week"
          series={activitySeries}
          bins={yearBins}
        />
        <div className="flex items-center gap-2 text-xs text-secondary">
          <Sparkline
            values={yearBins.map((bin) => bin.values[0] ?? 0)}
            label="Merged per week, 2026"
            className="w-24"
          />
          merged per week
        </div>
        <div className="flex items-center gap-2 text-xs text-secondary">
          <Sparkline values={[0, 2, 5, 0, 1, 0, 3]} className="w-16" />
          octo/app
        </div>
      </div>
    </Section>
  );
}

function Overlays() {
  return (
    <div className="grid grid-cols-3 items-start justify-items-start gap-8">
      <DropdownMenu defaultOpen modal={false}>
        <DropdownMenuTrigger
          render={<Button variant="outline">View options</Button>}
        />
        <DropdownMenuContent
          side="bottom"
          align="start"
          aria-label="View options"
        >
          <DropdownMenuGroup>
            <DropdownMenuLabel>Display</DropdownMenuLabel>
            <DropdownMenuCheckboxItem checked>
              Wrap lines
            </DropdownMenuCheckboxItem>
            <DropdownMenuItem>Expand all</DropdownMenuItem>
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuRadioGroup value="open">
            <DropdownMenuRadioItem value="open">Open</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="all" indicator="start">
              All comments
            </DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem>Collapse all</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Popover defaultOpen>
        <PopoverTrigger
          render={<Button variant="outline">Server status</Button>}
        />
        <PopoverContent side="bottom" align="start">
          <PopoverHeader>
            <PopoverTitle>Server healthy</PopoverTitle>
            <PopoverDescription>Local server is reachable.</PopoverDescription>
          </PopoverHeader>
          <PopoverSection title="Process">
            <KeyValueList>
              <KeyValue label="CPU">0%</KeyValue>
              <KeyValue label="Memory">48 MB</KeyValue>
            </KeyValueList>
          </PopoverSection>
        </PopoverContent>
      </Popover>
      <Tooltip defaultOpen>
        <TooltipTrigger render={<Button variant="outline">Sync</Button>} />
        <TooltipContent>
          Sync all checkouts <Shortcut keys={{ alt: true, key: "R" }} />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

const branches = ["main", "feat/rate-limit", "fix/auth", "docs/release"];

function Comboboxes() {
  return (
    <div className="grid justify-items-start">
      <Combobox items={branches} defaultValue="main" defaultOpen autoHighlight>
        <ComboboxTrigger
          render={
            <Button variant="outline" aria-label="Branch main. Switch branch">
              main
            </Button>
          }
        />
        <ComboboxContent className="w-64">
          <ComboboxSearch
            label="Filter branches"
            placeholder="Filter branches…"
          />
          <ComboboxEmpty>No matching branches</ComboboxEmpty>
          <ComboboxList aria-label="Branches">
            {(branch: string) => (
              <ComboboxItem key={branch} value={branch}>
                {branch}
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
    </div>
  );
}

function Dialogs() {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={setOpen}
      title="Delete comment?"
      description="This permanently removes the comment. This cannot be undone."
      confirmLabel="Delete comment"
      onConfirm={() => setOpen(false)}
    />
  );
}

/** Every primitive and composite variant, for review and visual goldens. */
export function Gallery({ page }: { page: string | null }) {
  return (
    <main className="min-h-dvh bg-bg p-6 text-fg">
      <h1 className="m-0 mb-6 text-xl font-semibold tracking-heading">
        diffx design system
      </h1>
      {page === "overlays" ? (
        <Overlays />
      ) : page === "combobox" ? (
        <Comboboxes />
      ) : page === "dialog" ? (
        <Dialogs />
      ) : (
        <Components />
      )}
    </main>
  );
}
