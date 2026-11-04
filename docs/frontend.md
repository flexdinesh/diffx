# Frontend conventions

## Experimental native desktop

`apps/desktop` is a Go module using MyGo native content windows. It renders no
webviews, HTML or JavaScript. Its `session` adapter starts a bundled matching
CLI or attaches to the existing compatible local scope; `workspace` owns
REST/SSE request lifetimes, cancellation and immutable presentation snapshots.
The server remains the owner of observations, review state, anchors and export.

`diffview` adapts stored patches to MyGo without recalculating their change
runs. Sparse old/new coordinate maps translate native buffer indices to stored
one-based lines. Context expansion inserts only verified unchanged captured
contents, then remaps selections and server-resolved annotations. Missing
contents stay unavailable. Editor mutation actions remain private and disabled.
Commit lenses disable review ranges while retaining native text copy.

The file list owns scrolling across files; individual native diff content does
not create nested vertical scrolling. Inline comment heights come from native
layout. Range selection uses click/Shift-click and keyboard controls. Unified
comment actions follow selection because MyGo's unified hover gutter cannot
reliably identify deletion-side lines. Horizontal offsets are retained while
mounted but cannot be restored across editor reconstruction through MyGo's
current public API.

Native primitives consume generated semantic tokens and palettes. Preferences
live in a versioned, atomically written app configuration file, independent of
browser localStorage. Server comments and reviewed-file marks are shared with
the web UI. Native tests use MyGo's headless tester; actual native-window
automation is a separate platform check. No memory or performance benchmarks
are part of the experimental port.

## Web application

Follow [DESIGN.md](../DESIGN.md). The shared design system lives in
`packages/ui` (`@diffx/ui`). Semantic tokens in `packages/ui/src/tokens.css`
are exposed as same-name Tailwind utilities (`bg-panel`, `border-control`,
`h-control`). Primitives in `packages/ui/src/components/ui` and composites in
`packages/ui/src/components` own all visuals; feature code arranges them and adds
semantic variants there when a look is missing. Keep authored CSS for
specialized layout, dynamic geometry, and Pierre's measured rendering boundary.
`mise run check:design` and oxlint enforce this.

The dev-only gallery renders every variant: run `pnpm dev` in `packages/ui` and
open `/design.html` (`?theme=dark`, `?page=overlays`, `?page=dialog`). It is not
part of the production build. Exhaustive gallery browser tests and screenshot
goldens are not maintained. Review affected variants in the gallery when changing
primitives. Node tests cover pure rules; React Testing Library with jsdom covers
production component/hook behavior that does not require layout. Keep critical
review journeys and actual rendering, geometry, scrolling, selection, responsive
and focus behavior in Chromium.

`App.tsx` composes page sections. `main.tsx` composes sidebar and appearance
providers around identity; `SessionProvider` owns context selection, routes
(`review-route.ts`) and collection (`use-collection.ts`) and imports no views;
`SessionShell` renders its loading, empty and failure states; `AppProvider`
keys workspace owners by context. Lint rejects import cycles, Hook-rule
violations and type assertions. The identity owner loads server mode and the current
account before mounting review state; distributed login and `/settings` work with
zero observations. Signed out, `/` renders the landing page (`Landing.tsx`,
with the Pierre demo passed in from `LandingDemoDiff.tsx` so DOM tests never
load Pierre) unless it carries a `?signin=` outcome; `/invite`, `/signup`
(while signup is on) and `/reset-password` render their pages and every other
path, including `/signin`, renders sign-in (sign-out and expired sessions
return to `/signin`):
Google first, and the password form while password signup is on, otherwise
behind a "Sign in with password" link (bootstrap `passwordSignupEnabled`). The app has no admin
pages; admins use the console. Invite and reset pages read their secret from the
fragment and drop it from history before any request. Account changes dispose workspace requests and event streams.
Local mode bypasses account flows. Consumers subscribe through domain hooks for diff source,
navigation, draft, review, reviewed files, and collapse state. Collapsed diff
files (per stream and scope), closed tree folders and Hide reviewed files (per
stream) persist in `sessionStorage`, so reloads and newer snapshots keep them within a tab; piped
snapshots key by context. One draft persists
across scopes. Context selection uses the Reviews column and rail
(`ProjectsNav.tsx`), the header repository and branch comboboxes
(`ContextIdentity.tsx`) and the popup picker (Cmd/Ctrl+K or the header's
“Search reviews” magnifier); `project-nav.ts` owns their grouping, ordering,
latest-stream and branch-filter rules. Selecting a stream keeps the column and
file sidebar open. `use-stream-nav.ts`, provided by `stream-nav-context.tsx`,
owns the column's rail/list state, width and grouping. `shell-policy.ts`
resolves review navigation once from the bootstrap (`input: "capture"` hides
all of it) and `use-shell-policy.ts` reads it; components read that policy,
never the deployment mode. The policy's `reviewColumn` is `always` remotely and
`multiple` locally: the session owner shows the column once `streamCount`
reaches two and keeps it for the page session. Its `collection` names the Sync
scope.
`use-sidebar.ts` owns file sidebar visibility and width and which mobile drawer
is open, with its focus. `Navigation.tsx` owns the island and both resize
handles. The selected context scopes every request. The session owner exposes
local checkout sync when bootstrap reports `collect`; loading `/` or a live
`/streams/{streamId}` URL, the header's “Sync checkout” or “Sync all checkouts” icon and Alt+R call
`POST /api/v2/collections`. Load-time sync runs in the background while a live
URL opens its stream's first available stored head; `/` waits for the
collection's `defaultContextId`. Collection completion never replaces an
open review; the newer-head notice offers “View latest”. A workspace skeleton
with “Collecting latest changes” appears only while no review is available.
Skeletons retain shell geometry and display preferences, hide decorative placeholders from assistive
technology, and respect reduced motion. Stored manifest loading uses the same diff
skeleton while keeping Pierre mounted. File previews replace their placeholders
individually as they arrive; loaded previews remain usable while others load.
The header busy indicator stays active through collection and preview loading.
The listener opens before background startup collection. Collection uses four
Git workers fed directly by discovery and publishes each completed checkout
immediately through one serial publisher. Discovery, Git reads and publication
overlap through bounded channels. Ingestion events refresh the catalog while
other checkouts continue; the batch response waits for every worker. Cancellation
and fatal publication errors cancel and join the workers before returning.
Load-time requests send `initial=true` to join startup once without collecting
twice; explicit Sync requests always collect fresh snapshots. The process scope
starts empty and widens with each published checkout, while discovery may continue. Startup failures keep the server
available and show Retry when no stored review can open. An explicit sync never
replaces the open review: it reloads the heads,
so the newer-head notice, drawer dots and global unread action announce new
snapshots, and feedback reports synced, updated and failed checkouts. The global
action opens a menu of unopened changed heads in activity order, naming each
head's repository, branch and worktree; choosing one opens that head. The drawer and
header share one per-browser seen-state owner; first visits baseline existing
heads and unchanged submissions do not create unread prompts. Activity ordering
uses catalog `lastChangedAt`, falling back to captured collection time, with
stable stream/context identity ties. Repositories use their most active visible
stream. Startup and root selection follow the same ordering. Collection triggers
are unchanged; no periodic collection or Git watcher is scheduled.
The “Switch review” picker first selects a repository,
then an immutable observation. Repository observations retain branch,
worktree, source/run, comparison, and collection-time metadata. Submission/session
associations remain separate from original snapshot metadata; catalog API filters
include `harness`, `sessionId`, `sessionName` and legacy `runId`. Global search is labeled
“Search reviews” and includes repository, branch, worktree, hostname, and source/run
labels. Piped history uses “Search Piped”, with collection timestamps newest first
and no repository filters. The header identifies these reviews with a terminal
icon, “Piped”, and collection time, without repository, branch, worktree, or directory.
The optional API `Context.source` distinguishes `local` and `stdin` independently
of its identity. Catalog presentation masks repository identity and stale state
on stdin observations. Repository
freshness, host, branch, worktree, and status filters never exclude Piped imports.
Catalog reads query stored metadata only; ingestion events, reconnect, and tab
visibility reload the catalog without replacing the selected observation.
Opening a review never discovers worktrees or reads Git. Repository results default to
Latest snapshots with changes. All / Latest / Stale filters freshness; the
right-aligned All checkbox also includes unavailable, empty, and unknown-status
snapshots. A newer collection supersedes older snapshots for the same owner,
source, repository, checkout, branch and comparison policy; arrival order breaks collection-time
ties. Freshness uses a durable stream head, including deduplicated collections and
observations outside the current search or page. Expiry and pruning leave that head
intact, so older retained snapshots stay stale. A newer collection of previous
content makes its existing review latest again; its original snapshot metadata stays fixed.
In the historical picker, changed observations come first, ordered by last submission time; counts
describe their fixed snapshots. Host, Branch, and Worktree filters apply before
repository grouping. Unavailable entries are disabled and skipped during keyboard
navigation. Filter selections persist while navigating and reopening the picker.
Switching disposes the previous workspace's request ownership and resets transient
state.
GitHub pull request streams read `observation.pullRequest`, which the server
projects from the stream head: `project-nav.ts` owns their title, metadata and
the rule that closed and merged pull requests leave current work;
`PullRequestBanner.tsx` renders the banner; `account-github.tsx` owns Connect,
follows, per-repository notification bells and Resync in Settings and renders
nothing when `GET /api/v2/github/connection` returns 404.
The PWA is remote only: `main.tsx` starts `pwa.ts` (manifest and Apple tags,
`/sw.js` registration, install prompt) and the boot-time push subscription
refresh only when the bootstrap mode is remote. `push.ts` owns browser support,
subscribe/unsubscribe and the device's server registration, including removal
before sign-out; `account-notifications.tsx` renders Settings › Notifications.
`sw.ts` is a separate WebWorker program (`tsconfig.sw.json`) built as the
import-free, fixed-name `dist/sw.js`; `push-notification.ts` holds its payload
guard and click rule. It has no fetch handler or cache.
The compact header exposes no snapshot delete action; theme choice lives in
the status bar's Settings popover. Snapshot deletion through the API removes its stored diff scopes,
comments, and reviewed-file marks.
Deletion preserves stream freshness history; older snapshots remain stale.
It leaves Git files untouched, and a new collection can create a review again.
The footer shows the selected scope's actual baseline. Its Diff comparison popover
explains the calculation and exposes the recorded base ref, base commit, merge base,
and HEAD with full commit IDs. These facts are persisted in observation metadata
and returned by `GET /api/v2/contexts/{contextId}` as `observation.comparison`
and `observation.head`; reading them never resolves current Git refs.

`DiffWorkspace.tsx` owns Pierre rendering,
versions, worker options, and measured geometry. `use-review.ts` and
`use-reviewed-files.ts` own their REST requests and recovery; reads cannot settle
across writes or owner disposal. `review-comments.ts` owns comment validation and
agent-facing formatting; `test/contracts/comments.json` holds examples checked
by both it and Go. Keep section-only state within its component.
Scrolling the diff selects the top visible file in navigation; selection advances
only after that file’s entire diff leaves the viewport. Scroll-driven selection
reveals its tree row without expanding the diff or navigating again.

See [architecture.md](architecture.md) for repository boundaries and dependency
rules.
