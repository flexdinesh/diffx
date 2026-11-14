---
name: diffx
description: Focused, compact, calm code review workspace
colors:
  diff-canvas: "#f2f2f3"
  diff-canvas-dot: "#d6d6d9"
  diff-canvas-dark: "#21252b"
  diff-canvas-dot-dark: "#3a3f49"
  shell: "#ffffff"
  shell-dark: "#3b414d"
  accent: "#3f5fd6"
  accent-dark: "#74ade8"
---

# Design system

## Design direction

diffx is a focused review workspace around [Pierre diffs](https://diffs.com).
The code is the primary content. Navigation, display controls, comments, and
review progress help people read and act on it.

Keep the interface **focused, compact, calm, precise, readable, and familiar**.
The direction is editor chrome in the spirit of Zed: panes meet flush at
hairlines, every corner is square, surfaces are One-style neutrals, and a single
blue carries selection and action. System UI type and monospace code stay. Polish
this direction rather than inventing a new one.

Avoid dashboard-like card grids, oversized headings, decorative gradients,
floating toolbars, curved corners of any size, decorative shadows, and colored
chrome competing with additions, deletions, syntax, or selected lines.

## Design principles

- Consistency over novelty. A different location does not justify a new style.
- Hierarchy before decoration. Code first, review actions second, metadata last.
- Spacing communicates grouping: close within a task, farther between tasks.
- Use semantic colors, not a convenient hex value or a syntax-theme color.
- Reuse shared primitives before adding component-specific variants.
- Keep density deliberate: compact controls, readable prose, generous hit areas.
- Responsive layouts preserve task order and access, not identical geometry.
- Respect Pierre's rendering contract. Visual changes must not break selection,
  annotations, sticky headers, context expansion, or virtual scrolling.

## Sources of truth

`packages/ui` (`@diffx/ui`) is the shared design system; apps import it and
never fork it.

Browser and desktop entries mount the same `packages/review-app` views and
styles. The MyGo webview host owns only OS menus, dialogs and session launch.
There is no second palette, native diff renderer or token generator. Preserve
Pierre's measured geometry, worker rendering, selection and theme behavior in
both entries. Desktop preferences start fresh with web settings; server review
data is retained.

- `packages/ui/src/tokens.css`: semantic colors, spacing, geometry, elevation,
  responsive gutters, light/dark equivalents, and theme palettes. It defines no
  radius tokens.
- `packages/review-app/src/theme.ts`: the theme registry (id, label, kind, mode and
  matching code theme).
- `packages/ui/src/typography.css`: fonts, text/icon sizes, weights, line heights.
- `packages/ui/src/styles.css`: Tailwind entry point, token-utility and shadcn
  theme bridge, and element defaults.
- `packages/review-app/src/style.css`: imports `@diffx/ui/styles.css`; shell, navigation,
  and viewer boundary.
- `packages/ui/src/components/ui`: repository-owned shadcn/Base UI primitives
  and shared variants.
- `packages/ui/src/components`: shared composites built from those primitives
  (see [Component inventory](#component-inventory)).
- `packages/ui/design.html` and `packages/ui/src/gallery`: the dev-only gallery
  of every primitive and composite variant; its goldens are visual contracts.
- `packages/review-app/src/review.css`: review/sidebar composition and touch adaptations.
- `packages/review-app/src/DiffWorkspace.tsx`: Pierre options, React slots, measured metrics.
- `packages/review-app/src/display-options.ts`: supported Pierre syntax-theme pairs.

Keep these responsibilities clear. Fix the original shared rule instead of
adding another late override. Existing short names such as `--bg`, `--fg`, and
`--panel` remain canonical semantic roles; do not create a second design-token
system for Tailwind or shadcn.

## UI stack and ownership

- React and Vite own application composition and delivery.
- Tailwind CSS v4 supplies utility styling through its Vite integration and the
  single global entry stylesheet, `@diffx/ui/styles.css`, which each app's
  stylesheet imports once; its `@source` generates the package's utilities.
- shadcn/ui uses Base UI for interactive primitives. Generated components are
  source code owned by this repository, not an opaque dependency.
- Add or update primitives through `packages/ui/components.json`, then customize
  the shared component source once to match this design system.
- Prefer a shared shadcn primitive and its variants for standard controls. Use
  authored CSS for shell/responsive composition, dynamic tree depth, resizing,
  measured geometry, status graphics, and Pierre integration.
- **Primitives own visuals.** Color, border, padding, height, type and
  icon size live in a primitive or composite variant. Feature code arranges
  components (layout, gap, margin, flex/grid, visibility) and may style its own
  intrinsic elements with token utilities; it never restyles a component through
  `className`, a hook class, `[data-slot]` or an ID. A missing look is a new
  semantic variant on the owning component, used everywhere it applies.

`styles.css` exposes each semantic token as a Tailwind utility under its own
name, so markup and tokens share one vocabulary. Default Tailwind scales are
cleared; only these exist:

| Utility family                     | Examples                                                                  | Token                                            |
| ---------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------ |
| Surfaces (`bg-*`)                  | `bg-bg`, `bg-panel`, `bg-raised`, `bg-hover`, `bg-selected`, `bg-shell`   | `--bg`, `--panel`, `--surface-raised`, …         |
| Text (`text-*`)                    | `text-fg`, `text-secondary`, `text-muted`, `text-accent`, `text-error`    | `--fg`, `--text-secondary`, `--muted`, …         |
| Borders (`border-*`)               | `border-border`, `border-separator`, `border-control`                     | `--border`, `--border-muted`, `--border-control` |
| Status                             | `*-success`, `*-warning`, `*-error`, `*-success-bg`, …                    | Semantic status pairs                            |
| Spacing (`p-*`, `gap-*`, `m-*`)    | `0.5`, `1`, `1.5`, `2`, `3`, `4`, `6`, `8`, `12`, `16`                    | `--space-*` (4px rhythm)                         |
| Sizes (`h-*`, `size-*`, `min-h-*`) | `control`, `control-small`, `control-compact`, `touch`, `icon`, `icon-sm` | Control, touch and icon tokens                   |
| Type                               | `text-xs`…`text-xl`, `text-code`, `leading-ui`, `tracking-label`          | `typography.css`                                 |
| Elevation                          | `shadow-overlay`                                                          | Overlay token                                    |

Use the utility when one exists; `bg-(--panel)` or `p-[var(--space-2)]` for a
token that already has a utility is rejected. 2px (`*-0.5`) steps are optical
insets reserved for `packages/ui/src/components/`.

`[data-theme]` always carries the resolved light/dark mode; the Tailwind
`dark` variant and Pierre's `themeType` read it. Palette themes additionally set
`[data-palette]`, whose `tokens.css` block remaps every semantic color role, so
components never branch on a theme. Do not add independent theme state. Tailwind Preflight and utility use must not override Pierre's
measured elements or replace the application tokens with framework defaults.

## Color

| Role                          | Token                              | Rule                                                               |
| ----------------------------- | ---------------------------------- | ------------------------------------------------------------------ |
| Application / primary surface | `--bg`                             | Inputs, header, primary content                                    |
| Diff canvas                   | `--diff-canvas`                    | Quiet neutral surround behind opaque file diffs                    |
| Diff canvas dots              | `--diff-canvas-dot`                | Faint dotted texture visible only between file diffs               |
| Diff empty-line hatch         | `--diff-buffer-stripe`             | Stripes on empty lines over the shell code surface                 |
| Changed-line number hue       | `--diff-number-base`, `-shell`     | Share of tint hue in number text; the rest mixes toward `--fg`     |
| Dark changed-row syntax       | `--diff-syntax-base`, `-shell`     | Share of token color in syntax text; the rest mixes toward `--fg`  |
| Secondary surface             | `--panel`                          | Sidebar, grouped controls, inline review annotations               |
| Shell canvas                  | `--shell-canvas`                   | Neutral background behind the header and flush panes               |
| Elevated surface              | `--surface-raised`                 | Dialogs and future actual overlays                                 |
| Primary text                  | `--fg`                             | File names, headings, review content                               |
| Secondary text                | `--text-secondary`                 | Supporting prose and available secondary actions                   |
| Muted text                    | `--muted`                          | Paths, counts, captions; still readable, never disabled by default |
| Separator                     | `--border-muted`                   | Pane dividers and low-emphasis grouping                            |
| Control boundary              | `--border-control`                 | Inputs and controls requiring stronger boundaries                  |
| Neutral hover                 | `--hover`                          | Available controls under the pointer                               |
| Count fill                    | `--count-bg`                       | Static count and badge surfaces; never hover feedback              |
| Inactive tab                  | `--tab-inactive-bg`                | Unselected sidebar tabs; `--shell-canvas` except in Diffx light    |
| Shell hover                   | `--shell-hover`                    | Hover/expanded header controls on `--shell-canvas`                 |
| Accent                        | `--accent`                         | Selection, focus, navigation links, primary commit action          |
| Accent interaction            | `--accent-hover`                   | Hover/active on filled primary actions                             |
| Selected surface              | `--accent-bg`                      | Selected file or pressed toggle                                    |
| On accent                     | `--on-accent`                      | Text on filled accent; never assume white in both themes           |
| Success                       | `--success`                        | Added files/counts, reviewed and resolved state                    |
| Warning                       | `--warning`                        | Modified files, recoverable notices                                |
| Review anchor                 | `--review-anchor`                  | Subtle tint on lines covered by a visible review comment           |
| Destructive / error           | `--error`                          | Deleted files/counts, conflicts, delete actions, errors            |
| Brand mark rows               | `--brand-removed`, `--brand-added` | The brand mark's changed rows only; never status                   |

- Use one foreground hierarchy across both themes. Dark mode maps the same roles;
  it is not a separate visual identity.
- Accent must explain an action or state. Do not outline every comment in blue.
  Open comments use a neutral edge; resolved comments add success plus text.
- `--file-code`, `--file-data`, `--file-markup`, and `--file-config` preserve small
  file-kind hints. They are a bounded categorical palette, not extra UI accents.
- Status always includes text, a sign, an icon, or a shape. Preserve staging's
  empty/half/full dot distinction; its half-fill is information, not decoration.
  Committed comparisons omit staging dots, legend, tooltip staging details,
  and staging descriptions in accessible file names.
- CPU and RAM in the status bar describe the Diffx server process, not
  the browser or whole machine. Keep that scope explicit in the status popup.
- Pierre owns syntax colors, changed-line fills, word highlights, line selection,
  and code-theme surfaces. App status tokens govern the surrounding UI only.
  Exception: palettes without a bundled Shiki theme render code through
  Pierre's CSS-variable theme (`diffx-palette`); their `tokens.css` blocks set
  `--diffs-token-*`/`--diffs-ansi-*` and point Pierre's addition, deletion and
  modified color overrides at `--success`, `--error` and `--warning`.

### Themes

- Settings offers three base themes (System, Light, Dark) and 22 palette
  themes. Each palette theme has a fixed mode. The choice is saved per
  browser (`theme-v2`); `index.html` applies it before the app renders and keeps
  its palette id lists synchronized with `theme.ts`.
- Palettes keep each theme's background, foreground and accent identity, then
  tune derived roles (muted, secondary, control border, on-accent, status and
  syntax colors) until every pairing meets WCAG AA. `design-refresh.spec.ts`
  asserts this for every theme.
- Status keeps its meaning: a palette's red, green and yellow back `--error`,
  `--success` and `--warning` only when their hues read as red (330–30°), green
  (80–170°) and yellow (30–70°) and stay distinct; otherwise use the Diffx value
  for that mode.
- The code theme defaults to “Match app theme”: Pierre for Diffx themes, the
  bundled Shiki theme where one exists, otherwise `diffx-palette`. Pierre and
  GitHub remain explicit overrides in View options. Saved code themes that no
  longer exist fall back to “Match app theme”.
- Theme swatches render through the theme's own tokens by scoping
  `[data-palette]` on the swatch; Diffx light and dark are addressable as
  `diffx-light` and `diffx-dark`. Never hard-code a preview color.
- Diffx light is white: content, header, sidebar, file headers and dialogs share
  `#fff`, separated by `#e3e3e5` hairlines. The diff canvas is the one grey
  field (`#f2f2f3`), so files read as white sheets on it. Because chrome and
  panels match, unselected sidebar tabs take a faint `--tab-inactive-bg`
  (`#f7f7f8`). Outline controls on panels stand apart by their 3:1
  `--border-control` edge, not by fill. Keep any new light grey at or above
  `#f2f2f3`.
- Quiet frames use the light canvas/dot tokens or their dark equivalents from
  the frontmatter. Keep dots behind opaque code surfaces; they do not tint code.
  Light mode uses the white primary surface and separator-colored dots; dark mode
  uses a deeper charcoal canvas and softened dots. File headers retain `--panel`.
- The docs site (`docs-website`) mirrors Diffx light and dark by overriding
  Vocs' `--vocs-*` variables in `src/pages/_root.css`, copied by hand from
  `tokens.css`: chrome on `--panel`, reading area on `--bg`, links on
  `--accent`. Update both when a Diffx light or dark value changes. The docs
  index is an ordinary docs page; the signed-out app landing is the one
  marketing surface.

## Typography

Use `--font-sans` for UI and review prose; `--font-mono` for code, paths, counts,
and captured context. Keep the root at `100%` to respect browser font preferences.

| Style               | Size token (default px)         | Weight / leading                                     | Use                                                |
| ------------------- | ------------------------------- | ---------------------------------------------------- | -------------------------------------------------- |
| Page/overlay title  | `--text-xl` (20)                | semibold / UI                                        | Empty states and dialogs                           |
| Workspace heading   | `--text-md` (14)                | semibold / UI                                        | Compact header title, future section headings      |
| Review body         | `--text-md` (14)                | normal / copy                                        | Comments, explanations, editor text                |
| UI body / label     | `--text-base` (14)              | normal or medium / UI                                | Buttons, tabs, file navigation                     |
| Small body / action | `--text-sm` (13)                | normal or medium / UI                                | Toolbar actions, select labels, comment actions    |
| Metadata / caption  | `--text-xs` (12)                | normal / UI                                          | Paths, counts, footer, state labels                |
| Code                | `--text-code` (13)              | normal / code                                        | Pierre rows; measured independently from UI        |
| Landing display     | `--text-display` (32–52, fluid) | semibold / `--leading-display`, `--tracking-display` | Signed-out landing headline only; never app chrome |

- Weights: `--weight-normal`, `--weight-medium`, `--weight-semibold` (400/500/600).
  Do not introduce incidental 550/650 weights.
- `--leading-ui` is 1.4; `--leading-copy` is 1.5. Use copy leading for prose,
  textareas, and preserved comment context.
- `--tracking-heading` gives display text a subtle negative tracking;
  `--tracking-label` is reserved for short uppercase section labels.
- Keep headings sentence case. Use semantic heading levels independently of size.
  Do not use uppercase as the default label style.
- Metadata is not body copy. Do not shrink functional text below `--text-xs`.
- `--text-input-touch` (16) is a functional mobile-input exception to prevent
  browser auto-zoom, not another heading/body style.
- Icons use `--icon-sm` (14), `--icon-base` (16), or `--icon-lg` (20).
  Brand/empty symbols are illustrations. SVG monogram text uses viewBox units.
- Code stays at `--text-base` with `--leading-code` (22px rows by default).
  Structured text uses bundled JetBrains Mono through `--font-mono`; tree text
  uses `--leading-tree`, with hit height separate from text leading.
- New type styles require a new semantic role, not a request for “slightly bigger.”

## Spacing

Use the rem-based scale: `--space-0-5`, `--space-1`, `--space-1-5`, `--space-2`,
`--space-3`, `--space-4`, `--space-6`, `--space-8`, `--space-12`, `--space-16`
→ 2, 4, 6, 8, 12, 16, 24, 32, 48, 64px by default. Keep 2/6px for compact
component internals; layout remains aligned to the 4px rhythm.

| Relationship                           | Rule                                      |
| -------------------------------------- | ----------------------------------------- |
| Icon / label, tightly related metadata | 4 or 8                                    |
| Related actions                        | 8; wrap whole actions when space runs out |
| Label to input; heading to prose       | 8                                         |
| Comment card/editor padding            | 8 block, 12 inline; sidebar and inline    |
| Sidebar section inset                  | 16                                        |
| Diff canvas gutters / file gaps        | 12; measured from `--space-3`             |
| Form groups / distinct sections        | 16 or 24                                  |
| Dialog padding                         | 24                                        |
| Large conceptual separation            | 32 or 48, rarely needed in review chrome  |

Use `--page-gutter` for notices and footer: 24 desktop, 16 below
1012px, 12 below 768px. Toolbar and file frames share `--diff-gutter`
(12px), accounting for the viewport's constant scrollbar inset. Header leading
content uses the same gutter; its trailing actions anchor directly to the
shell's right edge, independently of diff scrolling.
Use explicit margins instead of relying on browser-default paragraph spacing.

Custom values are acceptable only for documented optical or renderer geometry:
1px separators, 2px focus/segmented insets, small status marks, the 3px comment
edge, SVG coordinates, and the overlapping resize handle. Do not promote every
such detail into a token. New layout spacing must use the scale.

## Layout

- Use a full-width application shell, not a centered marketing container.
  The diff receives all width remaining after navigation. The signed-out
  landing page is the one exception (see
  [Signed-out landing page](#signed-out-landing-page)).
- The shell has no outer gutter and panes no gaps: header, navigation and review
  meet flush at 1px `--border` hairlines, without elevation. The file sidebar
  forms the navigation pane; notices, toolbar, viewer, and status bar form the
  review pane. The Review bench replaces both panes at `/`. Keep these hairlines outside Pierre's
  measured file geometry. At compact widths, remove the empty navigation pane and
  let the review pane fill the window; modal drawers span the window's full
  height from its leading edge, without clipping focus or menus.
- Header: `--topbar-height` (36 desktop, 44 narrow or coarse pointer). The
  header sits flush with the window edge directly above the panes, so its
  controls center between both edges. Keep
  desktop controls at 32px, header icons at 16px, the divider at 16px, and group
  spacing at 6px so the header recedes into the editor chrome. Toolbar and sidebar
  tabs share the compact 36px desktop rhythm (`--control-small` plus two
  `--space-1` insets). The narrow toolbar uses 44px rows matching the page header;
  its icon buttons match the header's 44px touch targets and 16px icons.
  Below 768px the header follows iOS bar metrics: action glyphs use
  `--icon-chrome-touch` (20px) and the review identity `--text-chrome-touch`
  (15px); drawer rows and the toolbar keep desktop glyphs.
- Safe areas: `viewport-fit=cover` lets chrome own the device insets
  (`--safe-top/right/bottom/left`, 0 off iOS). The header's `--shell-canvas`
  extends under the status bar (`--topbar-height` plus `--safe-top`) and the
  header is `position: sticky` at the top edge: Safari 26 ignores `theme-color`
  and samples only fixed or sticky edge elements for a solid status bar,
  otherwise blurring the header's controls. Status bars extend under the home
  indicator with content above it; drawers pad their top, bottom and leading
  edge; the app shell pads landscape side insets with canvas. A `theme-color`
  meta follows the active theme's `--shell-canvas` for the iOS status bar and
  browser chrome. Do not use `black-translucent`: it forces white status text
  over light themes.
  Uncommitted's subcontrol occupies a second row below the comparison picker
  and display actions. Its items fill that row without extra border/padding
  height; “All” retains the accessible name “All changes”.
- Header and display-toolbar icon actions share borderless ghost buttons,
  16px icons, and 1.7px strokes, including menu and dialog triggers. Hover and
  expanded states use the same neutral surface feedback. On mobile, both rows
  use 8px action gaps; the toolbar retains its diff gutter while trailing
  header actions reach the header's right edge.
- Header uses `--shell-canvas`, a quiet brand, and the review identity as its primary
  context. Keep the secondary scope heading and muted monospace path inline;
  hide the path at 768–1011px and both below 768px. Group ghost Refresh and
  sync actions at the trailing edge; share `--shell-hover` feedback with the
  trigger, since `--hover` is indistinguishable from the shell canvas.
  Keep the last header action's outer edge flush with the shell; never apply
  the diff scrollbar inset to header actions. Theme and snapshot deletion are
  absent from the header.
  A local checkout process replaces Refresh with an icon-only sync action
  (folder-sync icon), “Sync checkout” for one selected checkout and “Sync all
  checkouts” for discovered checkouts, that never swaps the open review.
- Header text uses cap-height trimming with token padding to center UI and
  monospace labels without clipping ascenders or descenders.
- Header lead: the brand mark, without the product name, and divider identify the
  app in every layout; the link's accessible name is “diffx home”. Below 768px
  the mark keeps a 44px target without moving. The file-sidebar toggle
  leads the review toolbar. A review's header then shows the Reviews
  breadcrumb before its identity. “Search reviews” (magnifier, Cmd/Ctrl+K)
  leads the header's trailing actions in every layout and opens the review
  picker. Account actions are the header's trailing action in every layout.
- Review navigation is a shell policy (`shell-policy.ts`), resolved once from
  the server bootstrap: checkout and remote processes switch between reviews;
  a capture (piped stdin or a fixture) serves one fixed diff. A capture shows
  no Review bench, Reviews breadcrumb, review stepper, search, Cmd/Ctrl+K,
  header comboboxes or sync; `/` opens its diff; its header keeps the brand, file-sidebar toggle and the
  static Piped identity, and the navigation pane holds only the file sidebar (hidden,
  still mounted, while collapsed). Piped wording, icons and grouping appear
  only there; elsewhere a stored stdin snapshot reads like any review.
  Components read the policy, never the deployment mode.
- Review bench: with review navigation, `/` lists every review stream's head
  with changes, for local and remote servers alike, even with one review; a
  capture's `/` opens its diff. Three panes meet at hairlines below the
  header, whose identity is the h1 “Reviews” (inbox icon) with sync when the
  process collects. A `--bench-facets-width` facet pane on `--panel` lists
  Views (“Needs attention”, “All open”, “Closed”, each with a count), then
  Repositories and, once an agent published, Agents; a facet filters on click
  and clears on a second click. The table pane on `--bg` has a toolbar (“Filter
  reviews…”, Alt+/, “Clear filters”, and a “Peek” toggle with Space), muted
  column headers (Review, Repository, Source, State, Files, Activity), and
  sections “New and updated” (with “Mark all viewed”) then “Everything else”
  (“Closed” in that view). Rows are `ListRow` links at tree density on one
  grid: unread dot, stream icon, title (semibold while unread; mono for
  branches), mono repository, source (“GitHub · @author”, or harness and
  host), state, file count and age; rows (`ListRow`'s `table` variant) and
  column headers end at the sections' 16px inset, so age aligns with “Mark all
  viewed”. One row is selected (`aria-current`,
  roving tab stop) and takes focus on load; ↑/↓/Home/End move it, Enter opens
  the review, Space toggles the peek, Esc closes it, Alt+Shift+V/U mark it.
  While the peek shows, a click selects and previews a row, and a double click
  or Enter opens it; without the peek (toggled off, or below 1012px) a click
  opens. Modified clicks keep the link's new-tab behavior.
  The `--bench-peek-width` peek on `--panel` shows the selected review before
  opening it, never marking it: origin, title, status badges, author or
  source, age; “Open review” (Enter hint), “Mark viewed”/“Mark unread” and “Open on
  GitHub”; changed files with +/−, commits, worktree and reviewed progress;
  the pull request description bounded and scrolling; agent sessions; and the
  top directories it changes. The view, facets and filter live in the URL
  (`/?view=open&repo=…&agent=…&q=…`, defaults omitted); views and facets add
  history, typing replaces it. Collection failures show above the list with
  Retry; empty views explain themselves (“You're caught up” offers “Show all
  open”), and without any pull request a GitHub server adds “Follow
  repositories in Settings.” The status bar keeps shortcuts, the review count,
  server status and Settings. Below 1012px the facets and peek leave; below
  768px a view menu leads the toolbar, column headers hide and rows wrap
  repository under the title. On GitHub servers the bench header's trailing
  actions start with a shell `LinkButton` “History” (history icon; icon only
  below 768px).
- History (`/history`, GitHub servers only) recaps the pull requests the
  reader's GitHub account opened and merged, in the bench's panes rather than
  a dashboard: no metric tiles, counts are facets. The header leads with the
  brand, a “‹ Reviews” `LinkButton` back to the bench's last URL, a muted “/”
  and the h1 “History” (history icon). The facet pane lists Range (Today,
  7 days, 30 days), Activity (Merged, Opened, each with a count; filter on
  click, clear on a second click) and Repositories with muted mono
  “merged · opened” counts under a matching legend. The table pane's toolbar
  holds ghost Earlier/Later icon buttons around the window label (“4 – 10
  Nov”, “Yesterday”) and “Clear filters”; Earlier disables once the previous
  window would start before retained history, Later at today. One sentence
  sums the window (“You merged 9 and opened 12 pull requests across 4
  repositories in the last 7 days.”), then a muted note when the window starts
  before retained history. Rows group under bench section headers by the day
  of their latest in-range event; `ListRow` table rows link to GitHub in a new
  tab: muted state icon, muted “#N” and title, mono repository, event
  (“Opened”, “Merged”, “Opened · Merged”) and mono time of day. The status bar
  states coverage (“Followed repositories · kept N days”) and the count. Not
  connected, not following, filtered-empty and empty windows explain
  themselves with Connect GitHub, Follow repositories, Clear filters or Show
  30 days. Range, end day, activity and repository live in the URL. Below
  1012px the facets leave for Range and Filter menus; below 768px rows wrap
  repository and event under the title.
- Reviews breadcrumb: a review's header leads with a shell `LinkButton` “‹
  Reviews” (chevron only below 768px; an unread dot while anything needs
  attention; Alt+Shift+B) and a muted “/” before the repository and branch
  comboboxes. It returns to the bench's last URL in this tab.
- Review stepper: the header's trailing actions start with a viewed toggle
  (“Mark viewed” check-check / “Mark unread” mail, `aria-pressed`,
  Alt+Shift+V/U) and Previous/Next review (Alt+Shift+K/J) around a muted mono
  “n of N”, stepping through the order the bench last listed in this tab; a
  review opened elsewhere leads the Needs attention order. Once every file is
  reviewed, a notice offers “Back to Reviews” and “Next review”. Below 768px
  the stepper hides; the breadcrumb returns to the bench.
- A GitHub pull request review shows a banner on `--panel` above the review
  notices: a state icon, muted `#N` and the semibold title, a status `Badge`
  (Open success, Draft neutral, Merged accent, Closed error), `@author`, muted
  monospace head → base, an outline-muted “Open on GitHub” `LinkButton`, and a
  “Show description” disclosure, collapsed by default and persisted per
  browser. The description is `MarkdownText`, bounded by `--reading-width`, and
  scrolls within the banner rather than pushing the diff away.
- Commits is a read-only lens over a branch's captured commits, never a review
  scope. The comparison menu lists it last, after a separator, with a count
  badge. A toolbar subcontrol (its own row below 768px, like the Uncommitted
  segments) holds outline-muted “Older commit” / “Newer commit” icon buttons
  (Alt+Shift+[ / Alt+Shift+], disabled at either end) around an outline-muted `ui/combobox`
  trigger: muted mono short SHA, subject, muted `n/total`. Rows show the short
  SHA, subject, mono green/red +/− (or “Merge commit”) and age, oldest first;
  the filter matches subject, author and SHA prefix, and a muted footer counts
  older commits that were not captured. Below the toolbar, a `--panel` banner
  in the pull request pattern shows a commit icon, muted mono SHA, semibold
  subject, “Merge commit” status `Badge` for merges, author and age, with a
  “Show message” disclosure: plain preformatted text bounded by
  `--reading-width`, scrolling within the banner. File headers replace Reviewed
  and file comments with an outline-muted “Open in Branch changes” action
  (“Open” below 768px); line selection, hide-reviewed and reviewed progress are
  absent. Merge commits and commits missing from the snapshot keep their
  selection and explain themselves in the empty state with a “View Branch
  changes” action. The footer reads `abc1234^ → abc1234`.
- A newer head for the viewed stream shows a one-line `--accent-bg` notice below
  the header with an outline-muted “View latest” action. Never swap the open review.
- Attention is per browser and account: a head is New until its stream was
  opened or marked viewed, Updated when its stream has a newer head than the
  one opened or viewed, and Viewed otherwise. Merged or closed pull requests
  and stale branches are Closed; drafts wait until ready. Only New and Updated
  need attention; the first visit counts work idle for a day as opened.
  Opening a review marks its head opened; peeking does not. Unread shows as
  accent dots on bench rows, the Reviews breadcrumb and header combobox rows;
  there is no header counter. Unchanged submissions must not create unread
  marks.
- Status bars: `--statusbar-height` (28: a compact control plus 2px insets,
  border included) for the sidebar, review and loading footers alike,
  with shortcuts shed before server status. At mobile widths or coarse pointers
  the review and loading bars become 44px rows below their hairline (iOS
  toolbar rhythm, matching the header) above `--safe-bottom`, with 44px trigger
  targets; the drawer's sidebar footer keeps desktop density but clears
  `--safe-bottom`. Below 768px status text is
  `--text-sm` and action glyphs `--icon-chrome-touch`.
  Server status is an icon-only compact ghost trigger at the trailing edge:
  success check when ready, reduced-motion-safe spinner while connecting, and
  error alert when metrics are unreachable; its accessible name carries the
  state. Hover, focus plus Enter, or tap opens a `--surface-raised` popover
  titled by the state, with a “Diffx server process” section listing CPU and RAM.
  Add future system details as further sections in the same popover.
  Settings follows it at the trailing edge: an icon-only compact ghost gear
  that opens a `--surface-raised` popover titled “Settings”, capped at 24rem
  and scrolling internally. Its Theme section is one flat choice list, base
  themes first; rows show a theme swatch, the name and a check on the
  current theme. Clicking or arrowing applies and saves immediately; there is
  no hover preview. Add future browser preferences as further sections.
- The diff display toolbar uses `--panel`, matching the header and status bar.
- Desktop navigation widths: `--sidebar-width` defaults to 330px.
  `Navigation.tsx` owns the pane; `PaneResizer.tsx` owns the shared resize
  handle; `use-sidebar.ts` owns the file sidebar width (200–520px), bounded by
  available viewport space. The bench's facets (`--bench-facets-width`, 240px)
  are fixed. The peek (`--bench-peek-width`, default 400px) resizes from its
  left edge (320–640px, saved; double-click resets) through
  `use-bench-peek-width.ts`, and gives way so the review table keeps at least
  30rem. The header file-sidebar toggle hides and reopens
  the file sidebar.
  Do not add competing CSS defaults or override a saved width at tablet sizes.
- Sidebar interiors share a 16px inset. Rows run edge to edge so selection and
  hover fill the pane up to its stable scrollbar gutter, and indent themselves to
  that inset. Tree rows use 8px base padding plus 16px per nesting level; file and
  folder names align to the same icon column.
- The viewport, file list, and comments list own their scrolling. Flex children
  need `min-width: 0` / `min-height: 0` so content can shrink without page overflow.
  Keep a 12rem minimum basis for sidebar content; the outer sidebar can scroll
  on short viewports or enlarged text so navigation/export/footer stay reachable.
- Scrollbars are thin and quiet, matching Pierre's code tracks: a fixed
  `--scrollbar-size` (10px) track with a 6px square thumb that appears while
  its pane is hovered or holds focus and darkens on hover and drag. The diff
  viewport keeps its thumb visible at rest, so its reserved track never reads as
  an empty gutter. Firefox uses
  `scrollbar-width: thin`; set `scrollbar-color` only there, since it inherits
  into Pierre's shadow roots and disables their Chromium styling. Long-lived panes
  that gain and lose overflow (diff viewport, file tree, comments, bench
  facets, table and peek) set `scrollbar-gutter: stable` so content and toolbar edges never
  shift. Menus, popovers, dialogs and the outer sidebar keep
  `auto`. Pierre owns its in-file horizontal tracks.
- `--reading-width` (680px default) bounds dialogs and explanatory text, never code.
- Use flex layouts for action groups; use grids only for genuine aligned data.
  Column gaps use the spacing scale. Do not turn every grouping into a card.
- Keep overlays anchored to their owning region. Sidebar/resizer layering uses
  `--layer-sidebar` / `--layer-resizer`; modal dialogs use the native top layer.
  Each resize handle overlaps its pane's trailing edge and adds no layout
  width. Drag distance changes that pane's width. A hairline separates navigation
  from the review pane; the measured 12px diff gutter remains inside that pane.

### Pierre boundary

- Prefer `CodeView` options and `renderHeaderPrefix`, `renderHeaderMetadata`, and
  `renderAnnotation` slots. Reuse the same comment components in slots and sidebar.
- Pass display preferences through the existing options and worker-pool theme
  integration. Preserve the user's split/unified, wrapping, and code-theme choice.
- Set supported `--diffs-font-*` / `--diffs-line-height` properties on `#viewer`.
  Keep the sizing probe, `ResizeObserver`, and `itemMetrics` synchronized.
- Pierre's header height is `2 * --space-4 + --space-1` (36px by default).
  Measure it as file spacing × 3 so browser font scaling updates CSS and
  `itemMetrics` together. Keep header-slot controls compact; do not apply global
  touch sizing to these slots. Collapse arrows share a `--control-height`-wide
  header prefix across files, independent of line-number gutter widths.
- Do not add outer margins, padding, or borders to virtualized `diffs-container`
  elements. Quiet frames use `--space-3` horizontal gutters on `CodeView` and
  Pierre-measured top/bottom padding and file gaps (12px by default), read from
  the sizing probe. Files stay separately framed boxes on the dotted canvas: frame
  each with `overflow: clip` and a 1px `--border` outline at the edge, matching
  shell separators in both themes.
  Remove bottom padding from code and diff surfaces. Native horizontal scrollbar
  tracks use the final row's addition/deletion color; other endings use the base
  code surface. Set `itemMetrics.paddingBottom` to the measured native scrollbar
  height for unwrapped code, or 0 when wrapped. The frame adds no layout geometry;
  keep `itemMetrics` synchronized so virtual scrolling remains correct.
- Under "Match app theme" with a base Diffx theme, `data-code-surface="shell"`
  puts context lines and gutters on `--bg` and separators on `--panel` through
  Pierre's `--diffs-*-override` properties, and stripes empty-line buffers with
  `--diff-buffer-stripe`. `--diffs-gap-style` and `--code-surface` repaint the
  lines and gaps Pierre fills with its theme background, near-black beside that
  surface; changed rows mix their tints with `--bg` too, so they never read darker
  than context. The review and landing diffs each carry these shadow-root rules,
  since `unsafeCSS` stays a checked literal. Syntax colors and addition/deletion
  hues stay Pierre's, with two readability mixes toward `--fg`: changed-line number
  text (`--diff-number-base-shell`, to keep AA) and syntax on dark changed rows
  (`--diff-syntax-base-shell`, since Pierre's dark syntax is tuned for near-black).
  Other code themes and palette themes keep their own surface.
- `unsafeCSS` squares every corner in the shadow root, including Pierre's inline
  diff spans and scrollbar thumbs. Pierre draws status icons from rounded sprites
  CSS cannot reach, so it hides each sprite and masks a square `--status-glyph-*`
  over the icon box, leaving its size unchanged. On the shell surface, header
  counts use `--success` and `--error` to keep text contrast.
- Keep `unsafeCSS` small and justified. Do not reconstruct syntax styling or
  broadly target internal shadow-DOM elements to make the library look like chrome.
- Each file header shows a Reviewed toggle and, when comments are enabled, an
  icon titled "Leave review comment on file". File comments use explicit
  `target: "file"`, appear in Pierre's lineNumber 0 annotation slot, and never
  select code lines. Navigation targets the file header; export omits line context.
- Changing code/header geometry requires validating navigation, sticky headers,
  annotations, expansion, wrapping, and browser font scaling together.

## Borders, radii, and shadows

- Use the shared `outline-muted` Button variant (`--border-muted`) for compact
  file-level diff actions, reset reviewed, and the toolbar comparison picker.
  Segmented layout controls and
  every `SearchField` (file filter and picker search) share this token.
  Text, icons, hover and focus identify these actions; avoid per-button colors.
- Use 1px `--border-muted` for separators and `--border-control` for interactive
  boundaries requiring 3:1 contrast. `--border` frames panes, files and
  cards; focus and errors use their semantic tokens. Do not stack a card
  border inside another bordered panel unless it
  represents an independent review object.
- Every corner is square: controls, cards, dialogs, status dots, the reviewed
  check, scrollbar thumbs, skeletons and Pierre's shadow DOM. There are no radius
  tokens or `rounded-*` utilities, and no exceptions. Custom SVG marks use square
  caps and miter joins; Lucide glyphs keep the geometry they ship with.
- `--shadow-overlay` is the single elevation treatment for drawers/dialogs.
  Use `--backdrop` for modal separation. Whitespace and surface changes do the
  grouping work in the rest of the app.
- Quiet frames use a neutral outline and measured gaps without an elevation shadow. The faint canvas dots
  use a 20px pitch and remain visible only outside file surfaces.

## Components

### Component inventory

Reach for these before writing markup or CSS. Each owns its visuals; extend it
with a semantic variant instead of overriding it.

| Need                                   | Component (`packages/ui/src/components`)                                                                                                 |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Navigation styled as a button          | `link-button` (keeps link semantics; `external` opens a new tab)                                                                         |
| Untrusted Markdown prose               | `markdown` (`MarkdownText`: no raw HTML, images as links)                                                                                |
| Action, link-styled trigger            | `ui/button` (variants: default, outline, outline-muted, outline-check, ghost, shell, rail, subtle, destructive, destructive-solid, link) |
| Icon-only action                       | `icon-button` (label is name and tooltip)                                                                                                |
| Primary action plus menu               | `split-button`                                                                                                                           |
| Pressed state / single choice          | `ui/toggle`, `segmented-control`                                                                                                         |
| Search or filter input                 | `search-field`                                                                                                                           |
| Text input, select, textarea, checkbox | `ui/input`, `ui/select`, `ui/textarea`, `ui/checkbox`                                                                                    |
| Tree, nav or picker row                | `list-row` (sans/mono font, depth, `option`)                                                                                             |
| Single choice among rows (themes)      | `choice-list` (check on the chosen row), `theme-swatch`                                                                                  |
| Panel title row                        | `panel-header`; tabs in `ui/tabs`                                                                                                        |
| Collapsible sidebar section            | `collapsible-section`, `disclosure` chevron                                                                                              |
| Count, status label, status dot        | `count-badge`, `ui/badge` (status tones), `status-dot`                                                                                   |
| Label/value pairs                      | `key-value-list`                                                                                                                         |
| Banner, empty, error                   | `notice`, `empty-state`, `error-state`                                                                                                   |
| Destructive confirmation               | `confirm-dialog`; other dialogs `ui/dialog`                                                                                              |
| Menus, popovers, tooltips              | `ui/dropdown-menu`, `ui/popover`, `ui/tooltip`                                                                                           |
| Menu with type-to-filter rows          | `ui/combobox` (shares the `ui/dropdown-menu` surface and rows)                                                                           |
| Group row: toggle plus its own action  | `disclosure-row` (chevron toggles; label acts)                                                                                           |
| Keyboard hint                          | `shortcut` (formats per platform), `ui/kbd` (solid buttons draw it in their foreground)                                                  |
| Loading placeholder                    | `ui/skeleton` (shapes mirror real geometry)                                                                                              |
| Divider, drawer backdrop               | `ui/separator` (`chrome` for the header), `scrim`                                                                                        |
| Review context icon                    | `context-icon`                                                                                                                           |
| The logo, alone or beside the name     | `brand-mark`                                                                                                                             |

Feature files may not render raw `<button>`, `<input>`, `<select>` or
`<textarea>`; lint points to the component. Review every variant in both themes
at `/design.html` (see `docs/frontend.md`).

### Buttons and toggles

- Reuse the shared shadcn `Button`, `Toggle`, and `ToggleGroup` primitives.
  Primary means commit the current task; destructive signals a destructive
  action. Add shared semantic variants instead of page-specific size variants.
- Standard control height is `--control-height` (32). Compact header-slot
  controls use `--control-compact` (24); segmented groups share the 32px outer size.
- Outlined buttons use regular borders, medium UI text, 12px horizontal padding,
  and square corners. Quiet buttons use secondary text and no resting border.
- Standard buttons and button-like header statuses use the 32px control height.
  Compact sizing is reserved for subordinate icon and metadata controls. Shared
  variants own hover, active, border, and background styling.
- Icon toggles take `touch` like icon buttons: 44px wide and high on narrow or
  coarse-pointer screens.
- Filled accent is reserved for the primary action, such as saving a comment.
  Refresh, copy/export, and layout preferences are secondary actions.
- Icon buttons have accessible names and centered icons; size the hit box
  independently from the icon. Do not replace labels with unexplained symbols.

### Forms

- Reuse the shared shadcn `Input`, `Select`, and `Textarea` primitives. They use
  `--bg` or `--panel` and `--border-control`. Textareas share the
  review body style and 8px/12px padding.
- Label every control. Placeholders are hints, not labels. Keep label-to-control
  spacing at 8px; the compact comment frame keeps 6px under its 24px header row.
- Every search or filter input is the shared `SearchField`: 32px
  (`--control-height`), leading search icon, optional shortcut hint and trailing
  actions. It shows its focus ring around the entire grouped field. Selects and
  textareas retain the shared visible focus ring, except the comment editor field,
  whose focus is a single accent border inside its frame.
- Keep Base UI values validated at application boundaries. New validation must
  pair `--error` with explanatory text and `aria-invalid` / `aria-describedby`.

### Cards, navigation, and status

- Review comments and editors use the shared shadcn `Card` composition: one flat
  `--panel` frame with a complete subtle border, no shadow, 8px block margins and
  8px/12px padding. A 24px header row, the body and the action row are separated by
  6px and 8px gaps, not bars or separators, so the frame takes little height from
  the code. Actions sit 4px apart so a saved comment's actions fit one row
  in the default 330px sidebar: compact icon buttons for Copy (XML), Copy link,
  Edit and Delete, a text-only ghost “Reply”, then the labeled outline
  Resolve/Reopen at the trailing group. Touch sizing may wrap the row. Saved
  titles state the target ("File", "Line 2–4") without repeating
  "Review comment", then “·” and the author: “You” for the user, or an agent's
  reported name after a lucide `Bot` icon. An unverified agent name carries the
  title and screen-reader description “Name reported by the agent”. “You” never
  truncates; an agent name shares the title's space with the sidebar location. Editors title themselves "New comment" or "Edit comment"
  beside the muted mono location, with copy at the header's trailing edge; the
  editor's label is screen-reader only. The focused editor field is the only
  accent: a single accent border, no offset ring. Keep status at the header's
  trailing edge and use compact labeled icons for saved comments' state and
  actions; editor actions are text-only.
- Comment actions use `xs` buttons with touch sizing on coarse pointers; each
  file's viewed control uses standard button sizing.
- Replies sit under the body in one flat list, each separated by a
  `--separator` hairline: a 24px muted `xs` meta row (author label, “·”,
  relative age with the full time as title, “· edited”) with trailing
  icon-only Edit (the user's own replies) and destructive Delete (any reply,
  confirmed by `ConfirmDialog`), then the body in review text. Agent comments
  and replies offer no Edit. “Reply” opens a compact composer below the
  thread that reuses the comment field, with text-only Cancel and primary
  “Reply” (⌘/Ctrl+Enter submits, Esc or Cancel closes and returns focus to
  Reply). Composer text survives reloads and live updates, and is shared by the
  comment's inline and sidebar cards. A resolved thread ends with a hairline
  muted line “Resolved by You|agent · age”; the status badge stays “Resolved”
  so the sidebar header keeps its location and author.
- Resolved comments collapse to their header by default. A labeled chevron
  expands the body and actions; reopening restores the expanded open state.
- Comment views default to open comments and offer Open, Resolved, Outdated, and
  All filters. The Comments panel's filter menu adds an Author group
  (Everyone, Me, Agents) after a separator, with the same radio items and
  counts; a second trigger would crowd bulk Delete off the 330px row. A chosen
  author follows the status in the trigger (“Open · Agents”); bulk Delete
  ignores it. The Comments panel lists Current comments, then an Outdated section
  for comments whose code changed or left the diff. Outdated comments carry an
  "Outdated" badge, a muted placement note, and their original code; they stay
  out of code lines and render in the file's lineNumber 0 slot. File headers and
  the file tree show muted outdated counts beside accent current counts. A
  snapshot-notice-style banner points to open outdated comments. "Keep open"
  moves an outdated comment to its file on the latest snapshot, still labeled
  outdated.
  Export uses one primary `Copy review` action plus a menu for broader scopes.
- The editor's primary action is `Comment` for a new comment and `Save` when
  editing, with a muted "⌘Enter to comment"/"to save" hint (platform format) on
  wide inline editors. Keep it enabled when idle; empty submission focuses the
  labeled editor and shows an inline error. Disable it while saving. Deleting a saved
  comment requires explicit confirmation.
- Tint every old/new diff row covered by a visible comment, including its
  gutter and multi-line range. Use a stronger green/red tint for addition and
  deletion rows; reserve `--review-anchor` yellow for unchanged context. Mix
  each tint with Pierre's existing background instead of replacing it.
- Navigation uses a quiet background, aligned text/icons, and a clear current
  item. Reviewed files retain readable text and a check; do not fade the entire row.
  The current item is the last file navigated to; scrolling the diff never
  changes it.
- “Hide reviewed files” is an icon toggle beside the file filter, matching the
  field's height and `--border-muted` boundary; pressed uses the accent toggle
  state. `useFileNavigation` owns one visible file set for the tree, diff list
  and Alt+J/K. Marking the file in view hides it at once and moves selection to
  its next visible neighbor. Empty results say “All files reviewed” with a
  “Show reviewed files” action; comment navigation to a hidden file shows them.
  When shown, marking a file scrolled past its top keeps its collapsed header
  at the viewport top, so the next file starts from its first line.
- Alt+M toggles the selected file's reviewed mark, owned by `useFileNavigation`
  with the header button's rule. When reviewed files are shown, a successful
  mark selects the next file; unmarking and the header button keep selection.
  Key repeat and in-flight writes are ignored. Read-only commit views ignore
  Alt+M and omit its hint.
- Navigating from the file list briefly pulses the destination file header with
  the accent for about one second. Reduced-motion mode uses static feedback that
  remains visible long enough to identify the destination without animation.
- Change navigation (Alt+[ / Alt+] and the toolbar arrows) steps through
  contiguous blocks of changed lines as one list across the visible, expanded
  files in tree order, owned by `useFileNavigation` with pure rules in
  `hunk-navigation.ts`. Collapsed files (including files reviewed and
  collapsed), placeholders and unloaded previews have no stops; Alt+J/K still
  reaches them. The last block navigated stays current while on screen;
  otherwise the viewport's top decides. File steps (Alt+J / Alt+K and the
  toolbar chevrons) share this rule with the selected file, scroll the file's
  header to the top, announce “File n of N: path” and do not wrap: “No previous
  file.” / “No more files below.” (naming Load more when previews remain). A block already fully visible does not
  scroll; others align with up to three context lines below the sticky header.
  The block's rows pulse like comment navigation, the tree selects its file,
  and a polite live region announces “Change n of N in path”. The ends do not
  wrap: “No changes above.” / “No more changes below.” feedback (naming Load
  more when previews remain), cleared by the next step.
- Sidebar tabs fuse with their panel: the active tab shares `--panel`, inactive
  tabs recess into `--tab-inactive-bg` and draw their own edges. The active
  tab's count badge takes the selected treatment (accent text on `--accent-bg`,
  soft accent edge); inactive counts stay neutral. A tab strip's header
  recesses into `--tab-inactive-bg` too, with the bottom hairline the active tab covers, so
  header actions share the inactive tabs' surface and use the `shell` button
  variant. Every tab keeps its trailing edge, separating it from the actions.
  Segmented choices and pressed toggles use accent text on `--accent-bg`. Expose state with native/ARIA semantics.
- Badges use compact text and neutral surfaces, except the active tab count
  above. Status color is
  supplemental; retain “Open”, “Resolved”, Git letters, counts, and staging shapes.
- Future menus or tables inherit these text, row, surface, and selection rules.
  Do not invent a separate menu/table palette or container style.

### Overlays

- The header identifies the open review after the brand divider at control
  height as two ghost `ui/combobox` triggers, repo ▾ / branch ▾: a semibold
  repository name with repository icon, then a quieter branch. Give the
  repository name priority when long context labels truncate. Below 768px
  they become one ghost title menu filling the header's free width: the
  semibold `--text-chrome-touch` branch with a chevron over the muted
  `--text-sm` repository, each truncating to one line, at the 44px touch
  height. It opens “Switch review”, which switches repository and branch
  together. Each combobox opens a `--surface-raised` popup matching
  `ui/dropdown-menu` with a focused filter field on top (“Filter
  repositories”; “Filter branches, worktrees, sessions…”), then the matching
  rows; the first match is highlighted so typing and Enter switch, and an
  empty filter result says so. The repository combobox opens a repository's
  most active stream; both list what the column lists, pull request filter
  applied. The branch combobox lists the open repository's streams
  with new marks, counts and metadata, matching every word against branch,
  worktree, host and sessions; when it has both pull request and machine
  rows they group under “Pull requests” and “From your machines” labels
  styled like dropdown labels, otherwise the list is flat. Both focus the column's current stream after
  switching. Worktree stays static.
  A capture uses static text: a terminal icon, “Piped”, and collection time;
  omit repository, branch, worktree, and directory identity.
  Cmd/Ctrl+K and the header's “Search reviews” action open the searchable
  “Switch review” dialog. Position it at
  `--topbar-height` plus `--space-6` on desktop (64px default), `--space-4` on
  mobile (16px default). Bound picker width to 35rem and viewport gutters. The
  picker owns focus and stays available during context errors. Choosing a
  review moves focus to the column's current stream.
- Search leads the picker; keep “Switch review” as its screen-reader title.
  Label search “Search reviews”.
  Use the shared `SearchField` (32px) with close at its right edge and the
  focus ring around its entire boundary. The dialog is `DialogContent`'s
  `command` variant.
  Use a flat, scrollable result list with changed checkouts first, then recency
  within each group; search preserves that order, using match strength to break
  recency ties. Compact rows (about 48px) put repository name and branch on
  one line, with a muted monospace path below; truncate long text to single lines.
  Mark linked paths with “Worktree”. Stored stdin snapshots follow the same
  grouping and filters as any review.
  Distinguish the current review with a check and accessible “Current review”
  text; retain explicit “Unavailable” feedback. Keep the footer to a short result
  count and keyboard hints.
  Show a compact trailing changed-file count across staged, unstaged, and
  untracked changes, independent of the current scope. Empty checkouts show
  “No changes”; unknown metadata shows “Status unknown”. Repository results default to
  Latest snapshots with changes. Put All / Latest / Stale in a compact segmented
  control below search, with a right-aligned All checkbox that includes unavailable,
  empty, and unknown-status snapshots. Keep freshness independent of that checkbox.
  Mark stale snapshot rows with an explicit trailing “Stale” indicator. Preserve
  the selected review when ingestion refreshes the catalog.
  Keep names readable and truncate paths before status, including on mobile.

- Use the controlled shadcn `Dialog` built on Base UI. General dialogs retain a
  visible accessible title; the review picker uses the screen-reader title
  above. Keep focus inside while open, support Escape, and restore focus on close.
- Use `--surface-raised`, `--shadow-overlay`, and `--backdrop`.
  Shadows communicate actual elevation only; no resting button/card shadows.
- Bound width by `--reading-width` and viewport gutters; bound height by the
  dynamic viewport and allow internal scrolling. Long XML must remain selectable.
- Navigation below 1012px is a modal drawer with backdrop, focus containment,
  explicit close, inert background, Escape support, and focus restoration.

### Account settings

- Settings (remote server only) is one page per section:
  `/settings/tokens`, `/settings/agents`, `/settings/github`,
  `/settings/notifications` and `/settings/password`. `/settings`, unknown pages and sections this server or
  account lacks open API tokens. Below the account header, the full-width pane
  scrolls so its scrollbar meets the window edge; the header's email keeps the
  diff gutter and truncates. Content is bounded at 64rem: a sticky 10rem
  section nav beside the account summary and the current section.
- The section nav lists only sections that exist (GitHub and Notifications
  stay absent on servers without them) as `rail` link buttons, marking the
  current page with `aria-current="page"`, as in the console. A link changes
  the URL, keeps focus and shows its section from the top; the nav never
  follows scrolling. Below 768px it becomes a wrapping row between the summary
  and the section.
- Every page opens with the account summary: the `h1`, “Signed in as” the
  account email (“with Google” for Google accounts), and an outline “Sign out”
  at the trailing edge.
- Sections follow in a fixed order: API tokens, Connected agents, GitHub,
  Notifications, Password (password accounts). Each sits above a separator in two columns,
  its `h2` and one short `--text-secondary` description in a 14rem column
  beside its state and controls; below 1012px they stack. Subsections use
  `h3`. Async feedback uses an always-mounted `role="status"` paragraph;
  failures use `ErrorState`.
- At most one filled primary action per page: the open token form's “Create
  token”, “Connect GitHub”, or “Turn on notifications”. Other actions are
  outline or ghost.
- Rows that can be removed (tokens, connected agents, other devices) and the
  GitHub connection
  keep their action in one ghost `…` menu (“Actions for {name}”), as in the
  console; Revoke…, Remove… and Disconnect… confirm with `ConfirmDialog`, the
  only place they turn red.
- API tokens list first: name, then muted kind and expiry (expired in
  `--warning` with text) and created and last-used dates. An outline “New
  token” opens the form in place and focuses Token name; Access and Expires
  after share a row where they fit; Cancel closes it and returns focus. A
  created secret shows once in a `--panel` status panel above the action, with
  focus on its Copy button.
- Connected agents lists MCP clients connected with OAuth: the medium-weight name
  with a status `Badge` (Verified in `success`, Unverified in `warning`), then
  muted lines for the verifying host and access in words (“Reads reviews” or
  “Reads reviews and adds comments”), and connected and last-used dates. Its
  `…` menu holds Disconnect…, confirmed by `ConfirmDialog`; success announces
  “Disconnected {name}.” in the always-mounted status. Empty, it is one line,
  “No connected agents.”, with an accent link to the MCP docs; no buttons.
- GitHub setup is a numbered list of two steps, each an `h3` with one line of
  prose: “Connect your GitHub account” (the connection row and `…` menu, or the
  filled “Connect GitHub” / “Connect GitHub again”) and “Choose repositories
  for the diffx app” (how many repositories are available once connected, and
  an outline external “Choose repositories on GitHub” link, always shown).
  Following that link reloads the Repositories list once, when the page is
  visible again; no check marks or progress indicators.
- Followed GitHub repositories carry a notification bell when the server sends
  push: a ghost `ui/toggle` (`icon-sm`, `touch`) named “Notify for
  owner/repo”, Bell when pressed (accent toggle state) and BellOff when muted.
  It updates optimistically and rolls back with an error on failure. Row labels
  match the bell's height so rows keep one rhythm, 52px on touch.
- A repository row shows warning “Access lost” for a lost follow, or muted
  “Pull requests off” (screen readers also hear that they appear once turned
  on) when GitHub has pull requests turned off; it stays followable.
- Notifications is scoped to this device first, then What notifies, then
  other devices, with at most one primary action. Its description points to
  What notifies, and GitHub when nothing is followed.
- What notifies (account-wide, all devices) opens with one line saying so and
  that pull requests agents open with the reader's GitHub account count as
  theirs. A `<table>` with a hidden caption crosses Opened (hint “New, reopened
  or ready for review”), New commits, Merged and Closed without merging with
  “By me” and “By others” `ui/checkbox` cells named “Opened, by me”; each whole
  cell is the label, 44px high on touch, and the table fits 320px. Then “Skip
  drafts until they're ready for review” and labelled `ui/input` lists with
  help text: Only from, Never from, Into branches, Mute titles containing.
  Everything autosaves: checkboxes optimistically, lists on Enter or blur after
  normalizing; saves are serialized, a failure rolls back with `ErrorState`
  and success announces “Saved” in the always-mounted status. Over-limit lists
  show an inline field error and do not save. It closes with a link to the
  bells in Settings › GitHub. Without a GitHub connection it shows only
  “Connect GitHub to choose…”; no buttons. This device shows one state: server push off, browser without push, iPhone or iPad Safari (Home
  Screen steps), blocked (site settings, no button), “Turn on notifications”
  (the only place the browser permission prompt is requested), or on: the
  “Send notifications to this device” checkbox and an outline “Send test
  notification”. An outline “Install app” appears while the browser offers
  installation; an installed app shows muted “Installed as an app.” Other
  devices list label, added and last-notified dates and a `…` menu with Remove….
  No banners or counters advertise notifications elsewhere.

### Sign-in pages

- Sign-in, signup, invite and reset pages share one narrow `auth-page` column:
  the brand link, an `h1`, short `--text-secondary` prose, then the methods the
  server enables. Google comes first; beside a password form it is an outline
  button so the form's submit stays the one primary action, and a separator
  divides the two. Links between these pages are inline accent links.
- Password sign-in always serves existing password accounts. While password
  signup is off, a quiet "Sign in with password" link button reveals the form
  and Google stays the primary action until it does.
- Password signup and first-admin setup require Email and Password, with native
  email validation and space for addresses up to 254 characters. Invite acceptance
  fixes Email to the invited address. Sign-in uses Email; accounts have no
  username.
- Pages for a method the server turned off render sign-in, like unknown paths.
  Link secrets come from the fragment and leave history before any request.
- On a remote server, signed-out `/` is always the
  [Signed-out landing page](#signed-out-landing-page); sign-in lives at
  `/signin`, and `/?signin=` still reports Google sign-in outcomes. Sign-in
  accepts `?next=` only for `/settings/github` and a pending OAuth consent
  page (`/oauth/consent?request=` and its 43-character ID).
- OAuth consent (`/oauth/consent`) uses the same `auth-page` column for a
  signed-in account; signed out, it opens sign-in and returns. The `h1` is
  “Connect {client} to diffx”. Identity comes first: a verified client shows a
  success Verified `Badge` beside its host; an unverified one a warning
  “Unverified app” `Badge` and “Name provided by the app”. Then “Will redirect
  to {host}”, then an `h2` “It will be able to” over a list: “Read your
  reviews, diffs and comments” with a check icon (always granted), and, only
  when requested, a ticked `ui/checkbox` “Add comments, reply and resolve”.
  Rows are control height, touch height on touch. Filled “Allow” is the one
  primary action beside outline “Cancel”; a muted closing line names the
  signed-in email with an inline “Sign out” link button that returns to
  sign-in for the same request. Loading is a `role="status"` line; load
  failures use `ErrorState` with Retry. An invalid, expired or answered
  request, or one refused before consent (unknown client or redirect), shows
  only “Can't connect this app” and one line telling the user to start again
  from their app; it never redirects.

### Signed-out landing page

The one Persuade surface: it tells what diffx does by showing a review being
read in the app's own window, then one band per feature, each showing that
feature at work, in the same world as the app.

- One boxed column, `--landing-width`, on `--landing-surface` (`--bg`, white in
  Diffx light). The box has 1px `--border` sides and a
  full-width hairline under every band; bands inset by `--page-gutter`. Bands:
  top bar, hero, showcase, the four feature bands and footer.
- Top bar: cells split by hairlines, editor-tab style: brand, the mono motto
  "A review bench for your changes." (hidden below 768px), mono Docs and
  GitHub links, and "Go to app" to `/signin`, filled with `--fg` on
  `--landing-surface`.
- Hero: the `--text-display` headline breaks at its sentence boundary at every
  width, then one `--text-lg` prose line. With public pull requests on, the
  labeled "Pull request URL" field with the one primary "Open" action, its
  inline error and the host tip sit beside the "Try one" grid: demo pull
  requests in one subgrid of icon, mono `owner/repo`, mono `#n` and title.
  With previews off, "Go to app" is the filled primary beside an outline
  "Read the docs".
- Showcase: the app's review window, whole and flush in its band (the box's
  sides and the band's hairlines are its edges): header, Changes tree with
  the app's file icons, Summary, status bar, and files on the dotted diff
  canvas. Alt+M marks the open file reviewed; it collapses to its header, the
  selection moves to the next file and the Summary counts it. The caption
  sits in its own hairline row below and labels it a demo.
- Features, in order: the Reviews bench (a branch arrives in Needs attention
  and Space peeks at it), GitHub pull requests (connect, follow repositories,
  pull requests arrive), comments (a line comment is written and saved) and
  agents over MCP (an agent finds, fixes, replies to and resolves the thread,
  and the review shows each step). Each band is two cells split by a
  hairline: its copy (an `--text-xl` heading, one prose paragraph and a docs
  link) padded at 5fr, its figure edge to edge at 7fr; they stack below 768px.
- Figures are faux product UI built with `packages/stageshow` on the app's
  tokens, file icons and status glyphs, never screenshots. Code takes Pierre's
  colors: palette themes' `--diffs-*` tokens, else pierre-light/pierre-dark
  values, with Pierre's change tints, gutter bars and commented-line tint.
  The page is the ground: no plate, no outline, no window shadow; a window
  sits a 32px gutter from its cell's top and sides and runs off its foot. Only
  a satellite laid over a window (the bench card on GitHub settings) takes
  `--shadow-overlay`. Each plays once when scrolled into view, shows its
  final frame under reduced motion, and has its own phone design. Figures
  are `aria-hidden`; the copy beside them carries the meaning. Real file
  icons keep their filled monograms beside outline folder icons, as in the
  app.
- Accent is limited to the primary actions, the selected demo states and inline
  prose links. Claims come only from the user docs; demo data is labelled;
  there are no feature card grids, kickers or decorative imagery.

### Public pull request previews

- `/{owner}/{repo}/pull/{n}` is a standalone review shell for everyone, like a
  capture: no Review bench, Reviews breadcrumb, stepper, search, comboboxes or
  sync.
  The header keeps the brand, a static `owner/repo` with `PR #n`, and the
  account slot: a "Sign in" button signed out, the account menu signed in.
  GitHub tab suffixes normalize to that path.
- Previews are read-only and say so by absence, not disabled controls: no
  comment gutters or cards, Reviewed toggles, Alt+M, "Hide reviewed files",
  Copy review or WebMCP. Comparisons offer Branch changes and Commits.
- The pull request banner shows as soon as metadata arrives, with a Refresh
  icon action and muted "Fetched … ago". An unchanged head announces "Up to
  date" politely; a new snapshot uses the newer-snapshot notice and "View
  latest", never swapping the open review.
- A preview notice in the same notice style explains the read-only view and
  offers its one way further, following your repositories: "Sign in" (to
  Settings > GitHub) signed out, "Open Settings" signed in. Dismissal persists
  per browser; the notice stays mounted and hidden.
- The first fetch keeps the diff skeleton under the banner; the status line
  reads "Queued", then "Fetching owner/repo from GitHub · 12s", adding after
  ten seconds that a large repository's first fetch takes longer. Refusals,
  failures and the fetch limit replace the skeleton with the page empty state,
  its explanation, and Retry or "Open on GitHub" where they help.
- When GitHub refuses every credential diffx reads with, the empty state reads
  "GitHub restricts this repository" and names the owner's limit. When the
  reader's own GitHub access could help, the filled primary action is that
  recovery: "Sign in" (to Settings > GitHub) signed out, "Open Settings" for a
  member without a GitHub connection, each beside "Open on GitHub". Once the
  reader's access was refused too, it offers "Open on GitHub" and Retry.

### Console

- The admin console (`apps/console`) is an Operate surface in this system's
  world: the same tokens, type and primitives, light or dark by the system
  theme, no palette picker. It is a document, not a workspace: the page
  scrolls under a sticky `--panel` header with the brand (“diffx Console”),
  the section links Accounts, Invites, Settings and Admin as `rail` link buttons
  (`aria-current="page"` marks the section), and the admin's email,
  hidden on narrow screens.
- Each section is one reading column (48rem) with an `h1`, short
  `--text-secondary` prose, then its form and list. Lists use separated rows:
  the name with status badges (You, Admin in accent, Disabled in warning,
  invite status), a muted detail line, and trailing actions. Account actions
  sit in one ghost `…` menu per row; the signed-in admin's row has none.
  Disable, delete and revoke confirm with `ConfirmDialog`; role changes,
  enabling and reset links act at once and report through an always-mounted
  `role="status"` paragraph.
- Invite and reset links appear once, in a `--panel` status panel with the
  link in monospace, outline “Copy link” and ghost “Dismiss link”.
- Signed-out pages (sign-in, setup, reset) follow the sign-in pages above.

## Logo and app icons

The mark is a thick X cut into five rows of code. The second row slides left in
red and the fourth slides right in green, each tilted 5°: one line removed, one
line added. All edges are straight, on a square tile. It has two drawings: the
full one, and a heavier one for 24px and below. The mark's red and green are the
one exception to a single blue, because the changed rows are the idea of the
mark; they are brighter than `--error` and `--success` and never carry status.

`apps/web/public/icons` holds the remote server's app icons. `icon.svg` is the
favicon in the small drawing, on `#f4f5f7`, switching to `#06080c` under
`prefers-color-scheme: dark`; `apps/console/public/icon.svg` and
`docs-website/public/icon.svg` are the same file. Rasters cannot switch, so they
stay on near-black: `icon-app.svg` is the source of `icon-192.png`,
`icon-512.png` and the desktop icons (`diffx-desktop.png` at 512,
`diffx-desktop.icns` holding 128 to 1024). `icon-maskable.svg` is full-bleed with
the mark at 74%, which keeps its corners inside the central safe-zone circle, and
is the source of `icon-maskable-512.png` and the opaque `apple-touch-icon.png`
(180). `badge.svg` is the white-on-transparent notification badge source of
`badge-96.png`; the rows' slide carries the mark without color. Regenerate the
rasters from their SVG source at their exact sizes when the mark changes.
The manifest uses `--bg` as background and `--shell` as theme color.

`BrandMark` is the mark in the product: before the name in every `.brand`, at
`--icon-base`, with slate rows in the brand's text color and the changed rows in
`--brand-removed` and `--brand-added`. It stays out of the accessibility tree
because the name is always beside it.

`docs-website/public` holds the horizontal logos (`logo-light.svg`,
`logo-dark.svg`, also used by the README) and the 1200x630 social card:
`og.svg` is the source of `og.png`, copied to `apps/web/public/og.png`. Their
text is outlined from Noto Sans, so they render the same everywhere. The web
app's `og:image` is the fixed URL `https://diffx.app/og.png`, because a crawler
needs an absolute URL; self-hosted servers share that image.

## Interaction states

| State    | Expectation                                                                         |
| -------- | ----------------------------------------------------------------------------------- |
| Hover    | Neutral surface feedback on available actions; preserve selected styling            |
| Focus    | `--focus-ring` with `--focus-offset`; never rely on hover or color alone            |
| Active   | Immediate surface feedback; filled primary uses `--accent-hover`                    |
| Selected | Persistent accent surface/underline plus `aria-current` or `aria-pressed`           |
| Disabled | Native `disabled`, subdued opacity, no hover, no pointer cursor                     |
| Loading  | `aria-busy`, status text, stable size, progress cursor, reduced-motion-safe spinner |
| Error    | `--error` plus a readable explanation and recovery action where applicable          |

Do not add motion for decoration. New animation must honor
`prefers-reduced-motion`; feedback must still work without animation.

### Loading

- Keep the real shell and Settings available while review
  data loads; the loading status bar carries the Settings trigger. Reserve both navigation widths and split/unified layout
  with neutral skeletons for review rows, file rows, the toolbar and diff frames.
  Narrow screens hide the placeholder file sidebar and follow the narrow layout.
- Skeletons use `--border-muted`, existing spacing tokens, and a gentle
  shared opacity pulse. Disable animation under `prefers-reduced-motion`. Keep
  placeholders out of the accessibility tree and tab order; label the busy region
  with a concise status at the bottom of the review surface. Its detail line
  pulses between `--fg` and `--muted` on the skeleton rhythm, static under
  reduced motion. Do not invent file
  names, counts, progress percentages or addition/deletion colors.
- Replace skeletons as data arrives, without a minimum display delay. Stored
  manifest loading uses the same diff-frame skeleton without unmounting Pierre.
  Open available stored snapshots while load-time Sync continues; use skeletons
  only for unavailable data. Replace each file preview independently as it arrives.
  Keep the header busy indicator active through collection and preview loading.
  Background Sync preserves the existing review and usable controls; newer heads
  use the existing “Newer snapshot” notice and explicit “View latest” action. Errors and
  empty results replace placeholders with their existing explanation and actions.

## Responsive design

- Use 1012px for drawer navigation/tighter gutters and 768px for compact mobile
  chrome. Synchronize drawer behavior with `use-sidebar.ts`.
- The compact comparison menu offers only captured choices: Uncommitted,
  Branch changes, Combined, Piped patch, or Commits. Selection closes the menu.
  A lone available comparison with no commits shows its name as plain text
  instead of a menu.
  Uncommitted exposes All / Staged / Unstaged in that semantic order when
  captured. The footer describes the selected comparison's captured baseline.
  An unavailable URL scope remains selected with an explanation and the
  available choices; do not silently substitute an empty comparison.
  Empty net comparisons point to captured changes in other scopes, including
  staged and unstaged changes that cancel in Uncommitted All.
- Keep comparison and desktop layout choice visible. View options and collapse/expand
  all are adjacent icon buttons with action titles. Put wrap, inline detail,
  code theme, and narrow layout choice in View options. “Previous file” /
  “Next file” chevron icon buttons (Alt+K / Alt+J), a short chrome separator,
  then “Previous change” / “Next change” arrow icon buttons (Alt+[ / Alt+])
  lead the toolbar's trailing actions. Change arrows show in every layout,
  disabled only when no expanded file has changes; file chevrons hide below
  768px, where the drawer and keyboard step files, and disable only without
  visible files.
  Below 768px the comparison truncates so the toolbar's actions keep one row.
- Mobile retains the brand mark and divider, and the review identity's
  repository/branch, as the title menu, or Piped/time context; truncate long
  labels and hide its shortcut hint. Hide the secondary heading/path and refresh
  text, preserving accessible labels and sidebar controls.
  Keep the linked-worktree icon visible when its text label is hidden.
- Collapse navigation to the toggle-controlled modal drawer; its width is
  `min(320px, 88vw)`. Keep file selection, comments, and export reachable there.
  Its tab strip matches `--topbar-height`, including the tablet header height.
  File/folder rows, filter, summary toggle, and review progress retain desktop
  control heights and spacing; do not apply the general mobile enlargement here.
- Hide redundant summary/shortcut detail before removing essential actions.
  Keep the Files changed total and inline added/deleted/modified/renamed counts
  visible in the drawer. Use file icons, semantic status colors, hover labels,
  and accessible names; wrap the group when the sidebar is too narrow.
- At mobile widths or coarse pointers, app-owned controls target 44px hit heights;
  icon controls also reach 44px width, including status bar triggers.
  Text-input sizing uses `--text-input-touch`.
  The compact View options and collapse/expand icons match the header
  controls' 44px size within the narrow toolbar’s first row, matching the header touch targets.
  Drawer navigation and summary controls retain desktop density, per the same brief.
- Pierre code rows and header slots retain measured geometry. Larger library
  gutter/header targets need a coordinated renderer-metric change, not a CSS override.
- Preserve the desktop split/unified and wrap preferences. Default narrow screens
  to unified without overwriting the desktop preference; allow a session override.
- Test narrow and short viewports, long paths/comments, expanded navigation,
  wrapped controls, and enlarged browser fonts. Shrinking text is not a fix.

## Accessibility

- Meet WCAG AA: at least 4.5:1 for normal text and 3:1 for meaningful control
  boundaries, focus indicators, and large text. Check actual adjacent surfaces
  in every theme, including selected and elevated surfaces.
- Use real buttons, inputs, selects, summaries, and dialogs. Modifier-based
  keyboard shortcuts avoid single-character activation conflicts. Preserve tree
  navigation, sidebar resizing, and Pierre's interaction model.
- Every icon-only action needs a name. Every form field needs a label. Connect
  dialog titles and validation messages programmatically.
- Focus must remain visible inside scrollable panes and on slotted review controls.
  Never remove an outline without providing an equally visible replacement.
- Aim for 44px touch targets; compact desktop actions must meet at least 24px.
  Audit Pierre-owned targets separately when changing library interaction geometry.
- Do not communicate reviewed, selected, staged, loading, or error state using
  color alone. Announce asynchronous feedback with the existing live regions.
- Respect root font preferences. Keep code metrics measured rather than assuming
  that rem always equals 16px; keep prose readable and long content wrappable.

## Rules for future UI work

> Do not introduce a new color, font size, spacing value, shadow, or
> component variant unless the existing system cannot express the required design meaning.

Before adding a value or visual pattern:

1. Does a token already express this role?
2. Does an existing component solve this?
3. Is the difference meaningful or incidental?
4. Does this compete with Pierre's code or duplicate its styling?
5. Does it preserve selection, annotations, and measured scrolling?
6. Does it remain coherent on mobile, with long content, and in every theme?
7. Are keyboard focus, contrast, labels, and hit areas correct?
8. Does this follow `DESIGN.md`? If a new role is necessary, update it here.

## Enforcement

`mise run check:design` scans CSS, TypeScript and TSX under `packages/ui/src`,
`packages/review-app/src` and every `apps/*/src`, including new nested files, static utilities, inline style objects, SVG colors
and Pierre's `unsafeCSS`. Local `mise run check` and explicit PR validation run the guard
and its contracts. It rejects literal palettes outside `tokens.css` definitions,
independent typography, literal radii/elevation, and arbitrary padding/margin/gap
lengths. Spacing utilities stay on the `--space-*` scale; 2px steps stay in
`packages/ui/src/components/`. Arbitrary values that name a token with its own utility must use
that utility. Compose authored values from the canonical tokens. Zero, automatic
spacing and 1px optical insets remain valid; dimensions, breakpoints and dynamic
measured geometry require review rather than a blanket literal-value ban.

The guard also enforces component ownership. Feature CSS may not select
`[data-slot]` or style by ID (except the `#viewer` Pierre anchor), and may not
set visual properties (color, background, border, padding, height, font,
outline, shadow, opacity) on a hook class that feature code passes to a
component. Feature `className`s on components may only arrange them. oxlint's
`react/forbid-elements` rejects raw form controls outside
`packages/ui/src/components/`.

The guard rejects any non-zero `border-radius`, `rx` or `ry` (CSS, inline style or
SVG attribute), any `--*radius*` custom property, and every `rounded*` utility,
including `rounded-none`. It has exact exceptions for SVG monogram units, icon line boxes, the Pierre gutter divider, Tailwind breakpoints, and
Pierre's line-surface mixes/inset strokes. Changes to exceptions need a concrete
role, rationale and contract test in the same PR. Do not exempt entire
components.

The development gallery remains available for reviewing variants and themes.
Exhaustive gallery browser tests and screenshot goldens are not maintained.
Use production component/hook DOM tests for behavior independent of layout;
retain Chromium coverage for critical review journeys, real rendering, geometry,
scrolling, selection, responsive behavior and focus. Full local checks keep these
critical suites mandatory, with one Chromium worker and heavy phases serialized
across linked worktrees.

Static checks cannot prove responsive behavior, accessibility, visual hierarchy
or correctness of styles assembled at runtime. Use existing browser tests and
review affected themes, narrow layouts and keyboard interaction. Changes to
Pierre geometry also require selection, annotation, expansion, wrapping and
scrolling checks. Record this evidence in the PR template.

## Audit baseline and consolidation

This audit records the starting points, not permission to reuse legacy values.

| Area             | Existing pattern / inconsistency                                                            | Consolidation                                                                                |
| ---------------- | ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Surfaces         | White / `#f8f9fb`; dark `#151619` / `#1b1c20`                                               | Retain neutral shell; add a semantic elevated surface                                        |
| Text             | `#24252a` / `#e1e2e7`; most supporting text shared `#787c86` / `#91949e`                    | Distinguish secondary/muted; strengthen muted contrast                                       |
| Borders/hover    | `#e5e7ec` / `#303137`; `#eeeef4` / `#282930`                                                | Retain separators; distinguish control boundaries                                            |
| Accent/brand     | Violet `#6260df` / `#aba7ff`, pale violet fills; former yellow logo                         | Blue `#3f5fd6` / `#74ade8`; `[x]` icons only, text-only header                               |
| Status           | Green `#24844c` / `#70cc95`, red `#cf4b51` / `#ed8a8e`, shared gold `#b58a29`               | Semantic success/error/warning with theme-equivalent contrast                                |
| File kinds       | Four inline light/dark blue, gold, orange, purple pairs                                     | Bounded semantic file-kind tokens                                                            |
| Type             | System sans/mono; 9, 10, 11, 12, 13, 14, 15, 17, 18px; 400/550/600/650/700 weights          | Five UI sizes; three weights; separate functional touch-input size                           |
| Leading/tracking | Browser defaults, 18/13 tree, 22/13 code, 1.5/1.6/1.7 prose; −0.6px and 1.1px tracking      | Explicit UI/copy leading, semantic tracking; preserve measured code leading                  |
| Spacing          | Repeated 3/5/6/7/9/10/11/14/15/17/18/20/22px padding, margins, gaps mixed with 4/8/12/16/24 | 4px rhythm; documented optical exceptions only                                               |
| Radii            | 3, 4, 5, 6, 7, 8, 10px plus circles                                                         | None; every corner is square                                                                 |
| Shadows          | Tiny button and segmented shadows; sidebar shadow; dialog backdrop                          | One overlay shadow; non-geometric file outlines without elevation                            |
| Geometry         | Header 58px, toolbar 40px; 24/28/30px icon controls and content-sized buttons               | 56/44px shell rhythm, 32px controls, 24px Pierre slots, 44px touch targets, safe-area chrome |
| Width/gutters    | Competing 260/220/280px sidebar rules; 10/12/14/22/24px gutters; 680px dialog               | One user-owned sidebar width; flush panes, shared reading width; measured 12px diff spacing  |
| Review           | Inline 12×14px padding versus sidebar/mobile 10px; 85px editor, 300px XML area              | Shared 8px comment inset, readable editor, viewport-bounded overlay                          |
| States           | Search removed input focus; textarea/select lacked shared focus; reviewed rows faded to 55% | Group/field focus, readable reviewed state, explicit destructive action                      |
| Responsive       | 1000/760px breakpoints; toolbar wrapped only below 760px                                    | 1012px drawer, 768px compact chrome, unified narrow default                                  |

### Decisions needing product judgment

- Validate compact density and the retained file-kind colors with real review
  sessions before adding a density preference or changing the brand direction.
- Larger Pierre gutter/header touch targets require a renderer-aware accessibility
  pass. Do not compromise scroll geometry to claim touch-target compliance.
- Keep all existing code-theme options; judge their syntax contrast separately
  from the shell when adding or upgrading a Pierre theme.
