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

- A developer runs `diffx` from a Git repository or workspace. The Go server opens the review UI in a browser and shows that checkout (plus its other worktrees with `--scan-worktrees`) or the checkouts and worktrees discovered under its paths, collecting the same scope again when the developer syncs. It opens on the Review bench, which lists what needs attention; picking one opens its diff.
- Reviews can cover all, staged, or unstaged changes. The file tree, reviewed-file marks, and comments help track progress.
- Comments can be copied as agent-ready XML. Compatible coding agents find the same review comments from a link, comment ID or their checkout, comment, reply and resolve through MCP; in supported browsers WebMCP offers reading and resolving.
- Comments are short threads: the developer and agents reply one level deep, each reply naming its author ("You" or the agent's reported name), and a resolution records who resolved it. Threads update live when an agent or another tab writes, and each comment has a copyable link that reopens the review at that comment.
- The CLI can also display a piped Git patch as a fixed review.
- A remote diffx server can collect from many agent sessions across many repositories. The Review bench keeps that work navigable as people hop between agents: it lists current changed branches newest first, separates what needs attention from what was already seen, filters by repository and agent, and peeks before opening. Inside a review, the Reviews breadcrumb returns to the bench, the stepper moves to the next review, the header repository and branch comboboxes filter as you type, and new arrivals are marked without replacing the review in progress. A piped patch shows only that diff.
- A remote diffx server is an installable web app. With an HTTPS public URL it sends push notifications, per device and opted in from Settings, for the followed GitHub pull request events each account chooses (opened, new commits, merged or closed, by the reader or by others, narrowed by author, base branch, title and draft filters; opened only by default). Pull requests agents open with the reader's GitHub account count as the reader's own. Local diffx has neither.
- On a remote diffx server with GitHub, History recaps the pull requests the reader's GitHub account opened and merged in followed repositories over Today, 7 or 30 days, paging back as far as admins keep history (30 days by default, up to a year). It is a recap for standups and self-reviews, not a productivity score.
- When its admins turn it on, a remote diffx server also previews any public GitHub pull request without sign-in: visitors paste its link on the signed-out landing page or swap `github.com` for the diffx host. Previews are read-only, shared by every reader and refreshed only on request; diffx comments are agent feedback, not GitHub review comments, so previews offer none and point visitors to following their own repositories instead.
- Review data is stored by the local diffx server, as documented in `../../docs-website/src/pages/api.md`.

## Capabilities and Constraints

- The web UI uses the Pierre diff package extensively. Use its intended APIs and rendering model; never break its selection, annotations, sticky headers, context expansion, or virtual scrolling.

## Evidence on Hand

- Product usage and behavior: `../../docs-website/src/pages/`.
- Architecture and frontend ownership: `../../docs/architecture.md` and `../../docs/frontend.md`.
- Agent integration: `../../docs-website/src/pages/mcp.md` and `../../docs-website/src/pages/webmcp.md`.
- Existing visual system and Pierre integration rules: `../../DESIGN.md` and `src/DiffWorkspace.tsx`.

## Product Principles

- Keep code and its diff readable as the primary content.
- Make review feedback precise and easy to hand to a coding agent.
- Respect Pierre's supported integration and measured rendering behavior.
- Keep review state clear across diff scopes and changing files.
