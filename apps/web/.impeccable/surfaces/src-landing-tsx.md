---
version: 1
slug: "src-landing-tsx"
primary_target: "src/Landing.tsx"
related_targets: []
---

Scope: signed-out `/` on a remote diffx server (always on in remote mode). Mode: Persuade, inside the established diffx world.

Audience: developers arriving from a shared link who do not know diffx. Job: understand what diffx does in seconds and try it. Action: paste a public GitHub pull request link and open its preview (when previews are on); otherwise "Go to app". Proof: a live Pierre demo diff with one static review comment and the agent XML it produces, labelled as a demo. Constraints: tokens, system type, violet accent, Pierre rendering contract; no invented claims (facts from docs-website pages only).

## Direction contract

THESIS: The page is one diffx review session in three acts, Run, Review, Hand off, shown on the real review surface; it refuses the headline-over-cards product page.

OWN-WORLD: diffx's neutral surfaces, shell-canvas island with 1px outline and lg corners, violet only for the primary action and the active act, Pierre diff colours, JetBrains Mono for commands, paths and XML, system sans for prose, one new display size for the headline.

STORY: Visitor reads the one-line promise, sees a real diff with a comment, sees that comment become agent instructions, then pastes their own PR or goes to the app.

FIRST VIEWPORT: Top bar: wordmark left; Docs, GitHub, outline "Go to app" right. Left-aligned display headline and one prose line, then the PR address field with primary Open and the demo PR label grid. Below, the stage island begins: sticky act strip (Run, Review, Hand off with diffstat graphic), the Run command row, and the Review diff with its comment card in view; Hand off starts below the fold.

FORM: Run, review, hand off; position 7 on the ordered list (dealt lead). seed:863da1b5efcce6c1. Signature interaction: the act strip tracks the act in view and jumps to an act on click; the Hand off XML reveals line by line once when it enters view (static under reduced motion).

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

Unresolved: none. Demo comment is static (user decision).
