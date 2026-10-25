# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

Developers checking Git diffs of their changes. They review the code in a browser and hand feedback to a coding agent.

## Product Purpose

diffx helps developers inspect local Git changes, leave review comments, and pass actionable feedback to a coding agent. A successful session lets the developer understand the changes and communicate the work they want done.

## Positioning

A local review workspace that combines a browser-based Git diff with comments that coding agents can consume. The diff is the center of the experience.

## Operating Context

- A developer runs `diffx` from a Git repository or workspace. The Go server opens the review UI in a browser, shows the checkouts and worktrees discovered under its paths, and collects them all again when the developer syncs.
- Reviews can cover all, staged, or unstaged changes. The file tree, reviewed-file marks, and comments help track progress.
- Comments can be copied as agent-ready XML. The same review comments are available to compatible coding agents through MCP and, in supported browsers, WebMCP.
- The CLI can also display a piped Git patch as a fixed review.
- A remote diffx server can collect from many agent sessions across many repositories. The Reviews column keeps that work navigable as people hop between agents: each repository lists only its current changed branches, newest first, the column collapses to repository marks that open each repository's latest work, the header repository and branch comboboxes filter as you type, and new arrivals are marked without replacing the review in progress. A piped patch shows only that diff.
- Review data is stored by the local diffx server, as documented in `../../docs-website/src/pages/api.md`.

## Capabilities and Constraints

- The web UI uses the Pierre diff package extensively. Use its intended APIs and rendering model; never break its selection, annotations, sticky headers, context expansion, or virtual scrolling.

## Evidence on Hand

- Product usage and behavior: `../../docs-website/src/pages/`.
- Architecture and frontend ownership: `../../docs/architecture.md` and `../../docs/development.md`.
- Agent integration: `../../docs-website/src/pages/mcp.md` and `../../docs-website/src/pages/webmcp.md`.
- Existing visual system and Pierre integration rules: `../../DESIGN.md` and `src/DiffWorkspace.tsx`.

## Product Principles

- Keep code and its diff readable as the primary content.
- Make review feedback precise and easy to hand to a coding agent.
- Respect Pierre's supported integration and measured rendering behavior.
- Keep review state clear across diff scopes and changing files.
