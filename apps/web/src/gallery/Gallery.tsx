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
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
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
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Kbd, KbdGroup } from "@/components/ui/kbd";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverSection,
  PopoverTitle,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Textarea } from "@/components/ui/textarea";
import { Toggle } from "@/components/ui/toggle";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import {
  ChoiceList,
  ChoiceListGroup,
  ChoiceListItem,
} from "@/components/choice-list";
import { CollapsibleSection } from "@/components/collapsible-section";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { ContextIcon } from "@/components/context-icon";
import { CountBadge } from "@/components/count-badge";
import { DisclosureIcon } from "@/components/disclosure";
import { EmptyState } from "@/components/empty-state";
import { ErrorState } from "@/components/error-state";
import { IconButton } from "@/components/icon-button";
import { KeyValue, KeyValueList } from "@/components/key-value-list";
import { ListRow } from "@/components/list-row";
import { Notice } from "@/components/notice";
import { PanelHeader } from "@/components/panel-header";
import { SearchField } from "@/components/search-field";
import { SegmentedControl } from "@/components/segmented-control";
import { Shortcut } from "@/components/shortcut";
import { SplitButton } from "@/components/split-button";
import { StatusDot } from "@/components/status-dot";
import { ThemeSwatch } from "@/components/theme-swatch";

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
              <span
                className={`${className} h-8 rounded-sm border border-border`}
              />
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
        <Row>
          <span className="size-8 rounded-xs border border-control" />
          <span className="size-8 rounded-sm border border-control" />
          <span className="size-8 rounded-md border border-control" />
          <span className="size-8 rounded-lg border border-control" />
          <span className="size-8 rounded-round border border-control" />
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
                <IconButton label="Collapse all folders" size="icon-sm">
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
          <Card active>
            <CardHeader>
              <CardTitle>Active editor</CardTitle>
            </CardHeader>
            <CardContent>
              <Textarea aria-label="Draft" />
            </CardContent>
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
          <ChoiceListGroup label="Diffx">
            <ChoiceListItem value="system">
              <ThemeSwatch palettes={["diffx-light", "diffx-dark"]} />
              System
            </ChoiceListItem>
            <ChoiceListItem value="dark">
              <ThemeSwatch palettes={["diffx-dark"]} />
              Dark
            </ChoiceListItem>
          </ChoiceListGroup>
          <ChoiceListGroup label="Omarchy">
            <ChoiceListItem value="tokyo-night">
              <ThemeSwatch palettes={["tokyo-night"]} />
              Tokyo Night
            </ChoiceListItem>
            <ChoiceListItem value="flexoki-light">
              <ThemeSwatch palettes={["flexoki-light"]} />
              Flexoki Light
            </ChoiceListItem>
          </ChoiceListGroup>
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
    </div>
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
      ) : page === "dialog" ? (
        <Dialogs />
      ) : (
        <Components />
      )}
    </main>
  );
}
