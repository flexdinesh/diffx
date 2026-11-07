---
version: 1
slug: "src-main-tsx"
primary_target: "src/main.tsx"
related_targets: []
---

# Sharp flush review workspace

Mode: Operate. Developers review local changes, navigate files, and leave feedback.
Scope: entire review shell, comment cards, code surface, app icons; docs site inherits corners and accent.
Chosen direction: user-selected flush shell from the approved in-app previews, with diff files kept as framed boxes.
Constraints: Pierre owns virtualization, sticky headers, wrapping, selection, annotations and measured geometry.

## Direction contract

THESIS: A code editor's chrome, not a web app's: panes meet at hairlines and nothing has a curve.

OWN-WORLD: Zed-inspired One neutrals (light #fafafa/#f0f0f1/#e2e2e3, dark #282c33/#2f343e/#3b414d), one blue accent (#3f5fd6/#74ade8), hairline borders, square corners everywhere including status dots and scrollbar thumbs, no elevation at rest. Palette themes keep their colors.

STORY: Navigate a file, read its diff, toggle Reviewed, and leave line or whole-file feedback. Comments read at the same density as the rest of the shell: 28px header, 8px body, 28px action row.

FIRST VIEWPORT: Flush header, fused sidebar tabs on the panel surface, full-width tree selection up to the scrollbar gutter, then diff files as separately framed boxes with 12px gaps on the dotted canvas. Under "Match app theme" with a Diffx theme, context code sits on the shell's surface.

FORM: Direction pinned by the user: flush shell plus framed files, kept dots, Zed-style blue, no corner exceptions, "[x]" mark in icons only with a text header. The Pierre layout and item metrics are unchanged. seed:2be7511a2ef76c37, rolled after the user pinned the structure; it records the roll and shaped nothing.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

Unresolved decisions: none.
