# Development

diffx is a Go server with a React/Vite web application embedded into the
release binary. Node and pnpm are build and test dependencies only.

## Install development version

Install the latest `main` changes that passed CI with Go 1.27.3 or later:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@edge
```

## Requirements

- [mise](https://mise.jdx.dev) 2026.8.6 or later
- C compiler for Go race tests
- Git
- Bash (included with Git for Windows)

Mise manages Go 1.27, Node from `.node-version`, and pnpm from `package.json`.
After cloning, trust the project config, install tools, then install dependencies,
hooks, and Playwright Chromium:

```sh
mise trust
mise install
mise run setup
```

`mise run` activates project tools without shell activation. For direct pnpm
commands, activate mise in your shell or use `mise exec -- pnpm <script>`.
List available tasks with `mise tasks`. Keep personal overrides in
`mise.local.toml`, which is ignored by Git.

`mise.lock` pins tool checksums for Linux x64 and macOS arm64; CI installs
with `--locked`. After changing a tool pin, including task-level tools, run
`mise lock` and commit the lockfile; `mise lock --bump` moves pins to the newest
allowed versions. `min_version` in `mise.toml` must match the mise version CI
installs and the requirement above; a contract test enforces this and that the
Dockerfile Go image matches the mise Go pin. `GOTOOLCHAIN=local` makes Go use
the mise pin instead of downloading a toolchain, so a `go.mod` bump past it
fails until `mise.toml` is updated. `mise run deps` installs pnpm dependencies
from the lockfile.

Build tasks declare `sources` and `outputs`; mise skips `web:build`,
`web:stage`, `build` and `plugins:build` when their source contents are
unchanged. Run `mise run --force <task>` to rebuild anyway. CI checkouts always
build.

## Commands

| Purpose                                                    | Command                          |
| ---------------------------------------------------------- | -------------------------------- |
| Start Vite HMR with a managed Go fixture server            | `mise run dev` or `pnpm dev:web` |
| Start a standalone Go fixture server with built web assets | `mise run dev:server`            |
| Generate the TypeScript API types                          | `mise run generate`              |
| Build the web application                                  | `mise run web:build`             |
| Build the dependency-free CLI at `dist/diffx`              | `mise run build`                 |
| Build and locally install the CLI                          | `mise run install`               |
| Build agent plugin adapters                                | `mise run plugins:build`         |
| Start the development Postgres (`compose.dev.yml`)         | `mise run db:up`                 |
| Stop it, preserving data / delete its data and restart     | `mise run db:down` / `db:reset`  |
| Run `diffx-server` on the dev Postgres behind Vite HMR     | `mise run dev:remote`            |
| Start the container server with its bundled Postgres       | `mise run docker:up`             |
| Stop the container server, preserving its database         | `mise run docker:down`           |
| Install Playwright Chromium                                | `pnpm test:browser:install`      |
| Run web unit and browser tests                             | `pnpm test:web`                  |
| Run Go tests                                               | `mise run test:go`               |
| Run boundary and shared-rule contracts                     | `mise run test:contracts`        |
| Check production package dependencies                      | `mise run check:boundaries`      |
| Check frontend design-token use                            | `mise run check:design`          |
| Run distribution API conformance tests                     | `mise run test:conformance`      |
| Run TypeScript checks                                      | `pnpm typecheck`                 |
| Run JavaScript linting                                     | `pnpm lint`                      |
| Format supported files                                     | `pnpm format`                    |
| Run every repository check                                 | `mise run check`                 |
| Run all pre-push checks, including Go race tests           | `pnpm check:push`                |
| Run lightweight CI checks                                  | `mise run check:ci`              |

Server-mode Go tests need Docker: `internal/testsupport/pgtest` starts one
pinned `postgres:18-alpine` container per test process (removed by
testcontainers' reaper) and gives every test its own database cloned from a
template migrated once per schema. Set `DIFFX_TEST_DATABASE_URL` to a
`postgres://` URL whose role may create databases to use a running server
instead, such as the development Postgres. Local-mode tests stay on SQLite.
`mise run check` and `mise run check:push` start one shared pinned Postgres
(`docker run`, loopback ephemeral port, tmpfs data, fsync off) after acquiring
the check lock and export its superuser URL as `DIFFX_TEST_DATABASE_URL` to every
step: Go, conformance and browser suites. The runner removes it on success,
failure and cancellation, and removes containers a killed run left for the same
Git common directory at the next start. Without Docker the run fails before the
first step. A preset `DIFFX_TEST_DATABASE_URL` is used instead, and no container
starts. `mise run check:ci` does not use the runner; its conformance tests start
their own containers.

Browser tests reuse the full check's web, CLI and `diffx-server` builds;
standalone browser runs build them when needed. Chromium uses one worker with an
in-memory fixture server behind a Vite preview on a free port. The remote spec
(`remote-server.spec.ts`) instead starts the worker's own `diffx-server` on a
fresh Postgres database, like the Go tests (Docker, or `DIFFX_TEST_DATABASE_URL`),
serves its embedded web build at the server's origin, and syncs with the CLI
without API mocks.

`mise run setup` enables the Husky `pre-push` hook. Every push runs
`mise run check:push`: all repository suites, boundary contracts and Go race tests, then rejects
uncommitted generated API types or built plugins. `mise run setup` installs
Playwright Chromium and stages embedded web assets; reinstall it after upgrading Playwright.
The hook needs mise on `PATH`; mise activates project tools. Checks run on your
local platform.

Both CI and release verification run `mise run check:ci`: static checks, web and
CLI builds, generated-file consistency, and the distribution API smoke test.
Local `mise run check` adds dependency/design guards, behavioral contracts,
JavaScript unit/DOM/browser suites, plugin tests, docs-site checks, release-tool
tests and all Go tests. These critical suites remain mandatory. Full checks and
pre-push queue the entire pipeline across linked worktrees using a process lock
under the Git common directory, before the first application build through the
last check and cleanup. Only compilation of the small check runner precedes the
lock, bounded to two Go workers. Every waiting invocation runs its own checks;
successful results are not shared between worktrees.
Heavy phases run sequentially, with Go limited to two concurrent packages and `GOMAXPROCS=2`;
another worktree waits instead of competing for CPU and memory. Cancellation
stops child processes before releasing the lock; failures propagate to the caller.
The lock coordinates linked worktrees using this runner, not independent clones
or direct standalone suite commands. Update older branches to include the runner
before relying on coordination with their hooks. Pre-push runs the Go tests with the race
detector instead. Go tests time out after 3 minutes (5 with the race detector) so a
hang fails fast. Keep these full suites and guards local. Chromium is installed
by local setup, not CI/release workflows. CI does not run a native OS test matrix.
GitHub's `ci-required` ruleset requires the lightweight `checks` status on an
up-to-date `main` change, including administrators; it is configured in GitHub,
not by mise. PR evidence records full local validation.

`mise run install` embeds the current web build and installs `diffx` into
`$GOBIN`, or `$GOPATH/bin` when `GOBIN` is unset. Ensure that directory is on
`PATH`.

The production web build is staged into `internal/webui/dist`, which is ignored
on `main`. Go builds, tests and vet embed that directory, so run
`mise run web:stage` after a fresh checkout or frontend change; without it Go
fails with `pattern all:dist: no matching files found`. CI commits the assets
only on release tags and the `edge` branch; see [release.md](release.md).

## Development data

Development uses `test/fixtures/sample.diff` with in-memory review state. The
Vite server starts and stops its own Go fixture process, so frontend development
does not need a real repository or persisted data.

Start the fixture server with built web assets:

```sh
mise run dev:server -- --no-browser
```

For simultaneous frontend development, choose a distinct Vite port in each
worktree. Mise forwards arguments after `--`; pnpm scripts take them directly:

```sh
mise run dev -- --port 5181 --strictPort
# Equivalent:
pnpm dev:web --port 5181 --strictPort
```

The managed Go fixture already uses `--port 0` and in-memory state. To run a
standalone fixture on a chosen port, or let the OS select one:

```sh
mise run dev:server -- --port 7983 --no-browser
mise run dev:server -- --port 0 --no-browser
```

For real checkout servers, build in each worktree, then run this in a separate
terminal per session (`mktemp -d` makes a unique runtime directory each time):

```sh
mise run build
env -u DIFFX_CONFIG_PATH -u DIFFX_LOCAL_STATE DIFFX_RUNTIME_DIR="$(mktemp -d)" ./dist/diffx . --port 0 --no-browser
```

Use the URL printed by each process; Ctrl-C stops only that session. A unique
`DIFFX_RUNTIME_DIR` isolates lifecycle discovery, default config, SQLite state,
producer identity and pending delivery state. Its database persists after exit;
reuse the directory to retain that session's history, or remove it after stopping
the process when no longer needed. Choosing a port alone does not isolate state.
Explicit `DIFFX_CONFIG_PATH` can select shared config, and its `local.state`,
`DIFFX_LOCAL_STATE` or `--state` can select a shared database despite distinct
runtime directories. Unset those overrides, as above, or give each session a
distinct state path. SQLite still permits only one owner per database.

`dev --fixture` collects its fixture before starting in the foreground and does
not use personal lifecycle discovery. `sync` collects only the selected checkout
against the default branch merge base, plus working changes. Hooks collect only
the triggering checkout. Use
`--base HEAD` for working changes only, and `--branch` for explicit object-only
recovery. `sync` submits to one configured destination and exits.
Redirected stdin (`git diff | diffx`) starts a foreground local process with
a fixed patch and ignores remote config. `diffx [PATH]` discovers and collects every
checkout and linked worktree under its paths at startup (current directory by
default; repeat `--path`, bound with `--scan-depth`), and opens fixed snapshots;
only an explicit `POST /api/v2/collections` discovers and collects them again.
Neither mode watches Git or automatically replaces the selected snapshot. Each
process lists only its discovered checkouts or capture from the shared database.
`sync` and `dev` reject several `--path` values and `--scan-depth`. A path
argument or `--path` cannot be combined with redirected stdin.
`CollectPatch` preserves the supplied patch without Git lookup;
its absolute submission directory is provenance only, not repository identity.
Use isolated
runtime directories (`DIFFX_RUNTIME_DIR`) and in-memory state for lifecycle tests; fixture processes
must not register inputs in the personal service. `DIFFX_EXIT_ON_STDIN_CLOSE`
is a foreground development-process lifecycle hook.

Stop running processes before upgrading. Local schema 11 through 15 databases upgrade in place to
schema 16 and survive restart; incompatible schemas are refused without resetting data. Local
`diffx` offers to back up older state and start fresh on a terminal;
`--reset-state` does so without asking, keeping only the newest backup beside
the database. Server Postgres state never resets; `diffx-server` migrates it at
startup. In-memory state does not survive restart. Local `local.retentionDays` defaults to seven; fresh
submissions apply the current setting, while exact retries preserve expiry.

For isolated production-user tests, start `diffx-server` with a temporary
persistent database and open the setup link it logs to create the admin. Sign in
and create API tokens through Settings, which works before any upload. New
accounts activate without a restart. Operator commands require stopping the
database owner.
Credentials scope REST, MCP, jobs and events. Local mode automatically supplies
its stable owner and exposes no account UI or management routes. Do not provision
against the running personal service.

## Server development

Server mode stores everything in PostgreSQL 18. `mise run dev:remote` starts the
development Postgres from `compose.dev.yml` (loopback port 54329, override with
`DIFFX_DEV_POSTGRES_PORT`), runs `diffx-server` against it and serves the app
through Vite HMR at `http://localhost:5173`, which also receives the setup
link. To run a built binary directly:

```sh
mise run db:up
DIFFX_SERVER_DATABASE_URL=postgres://diffx:diffx@127.0.0.1:54329/diffx?sslmode=disable \
  DIFFX_SERVER_INSECURE_COOKIES=true ./dist/diffx-server
```

Or use the checked-in Compose setup, which bundles its own Postgres:

```sh
mise run docker:up
docker compose logs server
mise run docker:down
```

The same server binary handles authentication, durable admission, workers and
queries in both setups. The Docker tasks stage embedded web assets before
building the image.

| Setting                         | Binary default | Container default | Flag                    |
| ------------------------------- | -------------- | ----------------- | ----------------------- |
| `DIFFX_SERVER_HOST`             | `127.0.0.1`    | `0.0.0.0`         | `--listen HOST:PORT`    |
| `DIFFX_SERVER_PORT`             | `7980`         | `7980`            | `--listen HOST:PORT`    |
| `DIFFX_SERVER_DATABASE_URL`     | Required       | Compose: bundled  | None (env only)         |
| `DIFFX_SERVER_RETENTION_DAYS`   | `7`            | `7`               | `--retention-days DAYS` |
| `DIFFX_SERVER_PUBLIC_URL`       | Unset          | Unset             | `--public-url URL`      |
| `DIFFX_SERVER_INSECURE_COOKIES` | `false`        | Compose: `true`   | `--insecure-cookies`    |
| `DIFFX_SERVER_GOOGLE_CLIENT_ID` | Unset          | Unset             | None                    |
| `DIFFX_SERVER_AUTH_RATE_LIMIT`  | `false`        | `false`           | None (env only)         |
| `DIFFX_SERVER_CLIENT_IP_HEADER` | Unset          | Unset             | None (env only)         |

The server listens on 7980 so a local `diffx [PATH]` (7981–7990) and the server
run side by side. The `DIFFX_SERVER_` prefix keeps deployment variables from
configuring the CLI, and the CLI's `DIFFX_LOCAL_*`/`DIFFX_REMOTE_*` from
configuring the server.

While no admin exists, each start logs a one-time setup link,
`<public URL>/setup#t=…`, valid for 24 hours; a restart replaces it. The link
creates the single admin with a username and password. No default credentials
are generated and there is no public signup. `DIFFX_SERVER_PUBLIC_URL` is the
browser-facing https origin used in that link (http only with insecure cookies);
unset, the link uses the listen address. An https public URL, or loopback http
for development, also enables Web Push notifications; the server generates its
VAPID keys once and keeps them in the database.

The admin signs in at `/admin/login`; every other account signs in with Google
and comes from an admin invite. Google sign-in needs
`DIFFX_SERVER_GOOGLE_CLIENT_ID`, `DIFFX_SERVER_GOOGLE_CLIENT_SECRET` (environment
only, never the config file) and `DIFFX_SERVER_PUBLIC_URL`; register
`<public URL>/api/v2/auth/google/callback` as the OAuth client's redirect URI.
Without them the server runs admin-only and cannot create invites. Create a named token in Settings. Use the
`collector` preset for producers, or `api` for review APIs/MCP. Token secrets are
shown once; expiry defaults to 90 days (selectable 1–365). Tokens cannot manage
accounts. Browser sessions are seven-day opaque cookies with CSRF protection on
unsafe requests. Secure cookies are the binary/image default; explicitly enable
insecure cookies only for HTTP development. Production requires HTTPS.

If the admin loses its password, reset it with the same
`DIFFX_SERVER_DATABASE_URL`; the command reads the password from stdin and
refuses non-admin accounts. It is one transaction, so servers may keep running:

```sh
diffx-server user reset-password --username alice --password-stdin < /path/to/private-password
```

Password reset revokes browser sessions.

Authentication rate limiting is off by default. `DIFFX_SERVER_AUTH_RATE_LIMIT=true`
allows each client IP 20 sign-in, setup and invite attempts per minute, counted
in Postgres so every replica shares the limit. The listener's peer is the
ingress, so it also requires `DIFFX_SERVER_CLIENT_IP_HEADER`, the header your
trusted ingress overwrites with the single client IP (for example `X-Real-IP`);
startup fails without it. Those routes answer 400 to a missing or invalid value,
such as a list or an address with a port. Never enable it where clients can
reach the server without that ingress, since they could then choose their own
IP. Each process also admits at most four concurrent authentications.

`/healthz` answers 200 while the process serves and never contacts the database;
`/readyz` answers 200 only while the database accepts queries, otherwise 503.
Both are unauthenticated, uncached and reveal no failure detail.

Precedence is defaults → explicit JSON file → env vars → explicit flags. Use
`--config-file FILE` or `DIFFX_SERVER_CONFIG` to select a server config. The file
supports `host`, `port`, `publicUrl`, `retentionDays` and
`insecureCookies`; the database URL is never read from it. The file
must exist and
can be mounted read-only; startup never creates it or a config lock. Personal
CLI config, `DIFFX_RUNTIME_DIR`, producer URL settings and external web assets
do not configure server mode. Restart to apply changes.

Production supplies its own PostgreSQL 18+ through `DIFFX_SERVER_DATABASE_URL`
(for example `postgres://diffx@db.example.com:5432/diffx?sslmode=verify-full`);
pool sizing uses URL parameters such as `pool_max_conns`. The role needs
`CREATE` on the database: the server creates and owns the `diffx` schema and
migrates it at startup. The image carries CA roots for verified TLS. Use a
direct or session-mode pooled connection; transaction-mode poolers (PgBouncer)
break session advisory locks and `LISTEN`. Back up with your database's tooling.
The checked-in Compose file bundles an example Postgres in the `diffx-postgres`
volume, never published; set `DIFFX_POSTGRES_PASSWORD`, or point
`DIFFX_SERVER_DATABASE_URL` at your own server. The server's root filesystem is
read-only and it keeps no local state. `mise run docker:down` preserves the
volume; `docker compose down --volumes` explicitly deletes it. Earlier SQLite
server volumes are not imported: an upgraded server starts empty.

Set `DIFFX_SERVER_PORT=7979 mise run docker:up` for a different published host
port; the container always listens on 7980. Compose also accepts
public URL, retention and insecure-cookie overrides,
from the shell or a git-ignored `.env` beside `compose.yaml`. Compose defaults to
insecure cookies because its HTTP listener is published on loopback; explicitly
set `DIFFX_SERVER_INSECURE_COOKIES=false` behind production HTTPS. The fixed
`diffx` project name keeps one
volume regardless of the checkout directory.
For other settings, use image env overrides or a Compose override file.
The GitHub integration adds the `github` profile: set every
`DIFFX_SERVER_GITHUB_*` variable and `DIFFX_SERVER_FETCHER_SECRET`, mount the
app's private key in an override file, then run
`docker compose --profile github up`. The `fetcher` service (`--target fetcher`,
Alpine with Git 2.44+) keeps mirrors in the `diffx-mirrors` volume, capped by
`DIFFX_FETCHER_DISK_LIMIT_GB` (default 20), and reaches the server's internal
port 7979 only on the `fetch` network.
SIGTERM stops HTTP and workers before releasing storage; unfinished admitted
jobs recover from the durable queue after leases expire.
Replicas are stateless and share one Postgres database; scale them freely.
`LISTEN` needs a direct or session-mode connection URL.

## Build pipeline

The production build generates API types, builds the web application, stages the
Vite output under `internal/webui`, and embeds those assets into the Go binary.
The resulting `dist/diffx` and `dist/diffx-server` executables have no
Node runtime dependency. Git is required only for producer-side checkout
collection. Local queries read SQLite and server queries read Postgres; neither invokes Git.

The OpenAPI contract in `packages/api/openapi.yaml` is the frontend/server
boundary. After changing it, run:

```sh
mise run generate
```

## Frontend conventions

Follow [DESIGN.md](../DESIGN.md). Semantic tokens in `apps/web/src/tokens.css`
are exposed as same-name Tailwind utilities (`bg-panel`, `border-control`,
`h-control`). Primitives in `apps/web/src/components/ui` and composites in
`apps/web/src/components` own all visuals; feature code arranges them and adds
semantic variants there when a look is missing. Keep authored CSS for
specialized layout, dynamic geometry, and Pierre's measured rendering boundary.
`mise run check:design` and oxlint enforce this.

The dev-only gallery renders every variant: run `pnpm dev` in `apps/web` and
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
zero observations. Signed out, `/setup`, `/admin/login` and `/invite` render
their pages and every other path renders Google sign-in; `/admin` renders only
for the admin. Setup and invite pages read their secret from the fragment and
drop it from history before any request. Account changes dispose workspace requests and event streams.
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

`cmd/diffx` resolves config → environment → flags once and composes producers;
`submission.Sync` pins verified manual retries to account/database identity, and
hooks deliver through the same `submission` destination. Hooks use session-scoped acknowledgements plus server reconciliation;
their private pending data retains a separate seven-day expiry. Token rotation
preserves a verified destination's pending data; offline captures with unknown
owners remain bound to their original credential until authenticated. REST and MCP
compose `reviewservice`; global `/mcp` provides catalog discovery and stored
diff/patch retrieval with explicit context IDs.

See [plugin setup](../docs-website/src/pages/plugins.md) for native harness installation, asynchronous
checkout sync, configuration precedence and hook diagnostics. `mise run generate`
and CI checks compile the adapters; web and CLI builds leave them unchanged; host SDK dependencies are development types only.
Pi and OpenCode's self-contained `dist/index.js` artifacts are committed so a
clone is ready to install without a build. After changing adapter source, run
`mise run plugins:build` and commit the regenerated artifacts. Codex and Claude
load their checked-in hook definitions through the root marketplace manifests.
