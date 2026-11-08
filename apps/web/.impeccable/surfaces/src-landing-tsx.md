---
version: 1
slug: "src-landing-tsx"
primary_target: "src/Landing.tsx"
related_targets: []
---

Scope: signed-out `/` on a remote diffx server (always on in remote mode). Mode: Persuade, inside the established diffx world.

Audience: developers arriving from a shared link who do not know diffx. Job: understand what diffx does in seconds and try it. Action: paste a public GitHub pull request link and open its preview (when previews are on); otherwise "Go to app". Proof: a live Pierre demo diff inside a static copy of the app window, one static review comment, and the agent XML it produces, labelled as a demo. Constraints: tokens, system type, blue accent, square corners, Pierre rendering contract; no invented claims (facts from docs-website pages only).

## Direction contract

THESIS: The page is one boxed column that shows a diffx review in the app's own window, then what it hands an agent; it refuses the headline-over-cards product page.

OWN-WORLD: diffx's neutral surfaces on `--landing-surface` (white in Diffx light), hairline box sides and band rules, Zed-style hairline header cells, square corners, accent only for primary actions, selected demo states and prose links, Pierre diff colours, JetBrains Mono for commands, paths, motto and XML, system sans for prose and the display headline.

STORY: Visitor reads the one-line promise, sees a real diff with a comment in the app window, sees that comment become agent instructions, reads the three steps, learns the CLI also works locally without signing in, then pastes their own PR or goes to the app.

FIRST VIEWPORT: Top bar cells: brand, motto "A review bench for your changes.", Docs, GitHub, filled "Go to app". Left-aligned display headline and one prose line, then the PR address field with primary Open and the demo PR label grid. Below, the demo window begins with its tree and the diff, the comment card fully in view at 1440x900.

FORM: App window demo, hand-off export below it, then Run / Review / Hand off cells. No scroll-driven motion.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

Unresolved: none. Demo comment is static; demo tree is synthetic (user decisions).
