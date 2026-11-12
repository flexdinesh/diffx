# Frontend conventions

## Desktop webview and shared review app

`packages/review-app` (`@diffx/review-app`) owns review views, state, request
lifetimes, styles, themes, workers and Pierre integration. It exports
`ReviewApplication({ platform })`; `apps/web/src/main.tsx` and
`apps/desktop/src/main.tsx` compose distinct entry points. `packages/ui` remains
the design-system owner. Shared feature code renders its primitives and uses
the same measured geometry.

`apps/desktop` is a separate Go module containing a thin MyGo webview host.
The host embeds the desktop Vite build, serves it on a loopback origin and
proxies the shared public review REST/SSE allowlist through native Unix, pinned
TLS or SSH-forwarded loopback connections. Default local sessions require no
viewer port. TLS credentials stay native, outside React and browser URLs. Its
typed bridge exposes launch state, folder selection, retry, target-switch
confirmation and explicit replacement.
OS menus and file dialogs remain native. The session adapter starts the bundled
matching CLI or attaches to a compatible scope; cancellation joins stale work,
and Quit stops only an owned local server. One desktop window focuses the same
target; changing workspace or connection requires confirmation. Cancel preserves
the current session. Accepted switches cancel and join requests and streams
before changing the target. Remote reconnect backoff caps at 15 seconds for two
minutes, then offers Retry; failed mutations are never automatically replayed.
Desktop acceptance returns the invoking CLI before server readiness; later
failures stay in the window. Server data and review behavior have one
owner. There are no custom Go diff widgets or generated native palettes.

Desktop uses the web preference implementation in its own webview origin.
The host retains its loopback port across restarts so appearance and display
preferences remain available. A saved-port conflict fails explicitly instead
of selecting another origin.
There is no migration from old native settings. Browser and desktop preferences
are separate; saved server comments and reviewed marks remain available.
Only the browser entry activates the remote PWA/service worker. Desktop requires
a modern WebKit runtime supporting the shared build target, Safari 16.4 or
equivalent; Linux acceptance uses WebKitGTK 4.1. A Linux build or cross-compile
does not establish macOS runtime acceptance.

`desktop:frontend` builds `apps/desktop/dist`; `desktop:stage` copies it into
`apps/desktop/internal/assets/dist` for Go embedding. These outputs stay ignored.
`check:desktop` covers the host, bridge and real local-session boundary;
`check:desktop:window` exercises real WebKitGTK windows; production excludes
automation and inspector. See [desktop-webview.md](desktop-webview.md).

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

`App.tsx` composes page sections. The shared application entry composes sidebar and appearance
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
across scopes. `/` is the Review bench (`ReviewBench.tsx`) wherever the
shell offers review navigation: facets, a sectioned review table and a peek.
`review-bench.ts` owns its pure rules (attention state, views, sections,
facets, query string, first-visit baseline); `use-bench-marks.ts` owns the
per-browser opened and viewed heads (`projects-seen[:account]`,
`bench-viewed[:account]`) behind the session catalog's `marks`, `markViewed`
and `markUnread`; `bench-trail.ts` keeps the bench's last URL and row order
in `sessionStorage` for the Reviews breadcrumb and review stepper
(`review-navigation.tsx`); `ReviewCompleteNotice.tsx` offers the next review.
`/history` (`HistoryPage.tsx`) renders outside `SessionProvider`, like
`/settings`, only when the remote bootstrap reports `githubEnabled`; other
servers redirect it to `/`. `history.ts` owns its pure rules (local-day
windows, stepping, query string, labels, day grouping, filters); the page reads
`GET /api/v2/github/history` and reuses the bench's panes and facet rows.
The header repository and branch comboboxes (`ContextIdentity.tsx`) and the
popup picker (Cmd/Ctrl+K or the header's “Search reviews” magnifier) switch
reviews too; `project-nav.ts` owns their grouping, ordering, latest-stream and
branch-filter rules. The shell policy's `pullRequests`, from bootstrap
`githubEnabled`, enables the bench's follow guidance and the branch
combobox's “Pull requests” / “From your machines” groups. `shell-policy.ts`
resolves review navigation once from the bootstrap (`input: "capture"` hides
all of it) and `use-shell-policy.ts` reads it; components read that policy,
never the deployment mode. Its `collection` names the Sync scope.
`use-sidebar.ts` owns file sidebar visibility and width and its mobile drawer,
with its focus. `Navigation.tsx` owns the navigation pane and its resize
handle. The selected context scopes every request. The session owner exposes
local checkout sync when bootstrap reports `collect`; loading `/` or a live
`/streams/{streamId}` URL, the header's “Sync checkout” or “Sync all checkouts” icon and Alt+R call
`POST /api/v2/collections`. Load-time sync runs in the background while a live
URL opens its stream's first available stored head; the bench at `/` lists
heads as they arrive and preselects the collection's `defaultContextId`. Collection completion never replaces an
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
stream. Startup and root selection follow the same ordering; the root
(`defaultContext`) prefers current work and opens a closed or merged pull
request only when nothing current changed. Collection triggers
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
projects from the stream head: `project-nav.ts` owns their title, metadata,
the rule that closed and merged pull requests leave current work and the
filter that shows them;
`PullRequestBanner.tsx` renders the banner; `account-github.tsx` owns Connect,
follows, per-repository notification bells and Resync in Settings and renders
nothing when `GET /api/v2/github/connection` returns 404.
The PWA is remote only: `main.tsx` starts `pwa.ts` (manifest and Apple tags,
`/sw.js` registration, install prompt) and the boot-time push subscription
refresh only when the bootstrap mode is remote. `push.ts` owns browser support,
subscribe/unsubscribe and the device's server registration, including removal
before sign-out; `account-notifications.tsx` renders Settings › Notifications
and `account-notification-preferences.tsx` its account-wide What notifies
(`/api/v2/github/notifications`), which owns the serialized autosave queue and
list normalization mirroring the server's.
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
Selection changes only through explicit navigation; scrolling the diff never
selects a file. File and change steps share `stepInViewport` in
`hunk-navigation.ts`, resolved per step from Pierre's measured offsets.

See [architecture.md](architecture.md) for repository boundaries and dependency
rules.
