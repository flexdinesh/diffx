# Development

diffx is a Go server with a React/Vite web application embedded into the
release binary. Node and pnpm are build and test dependencies only.

## Install development version

Install the latest `main` changes that passed CI with Go 1.25 or later:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@edge
```

## Requirements

- [mise](https://mise.jdx.dev) 2026.8.6 or later
- C compiler for Go race tests
- Git
- Bash (included with Git for Windows)

Mise manages Go 1.25, Node from `.node-version`, and pnpm from `package.json`.
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
| Start the container server with persistent SQLite          | `mise run docker:up`             |
| Stop the container server, preserving SQLite               | `mise run docker:down`           |
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

Set `DIFFX_TEST_PORT` for a separate browser-test server (default 4173), e.g. `DIFFX_TEST_PORT=4183 mise exec -- pnpm --filter @diffx/web test:browser`.

`mise run setup` enables the Husky `pre-push` hook. Every push runs
`mise run check:push`: all repository suites, boundary contracts and Go race tests, then rejects
uncommitted generated API types or built plugins. `mise run setup` installs
Playwright Chromium and stages embedded web assets; reinstall it after upgrading Playwright.
The hook needs mise on `PATH`; mise activates project tools. Checks run on your
local platform.

Both CI and release verification run `mise run check:ci`: static checks, web and
CLI builds, generated-file consistency, and the distribution API smoke test.
Local `mise run check` adds dependency/design guards, behavioral contracts,
JavaScript unit/browser suites, release-tool tests and all Go tests; pre-push
adds Go race tests. Keep these full suites and guards local. Chromium is installed
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

Stop running processes before upgrading. Current schema 11 databases survive
restart; incompatible schemas are refused without resetting data. To start
fresh, back up the state directory while stopped and explicitly choose a new
database path. In-memory state does not survive restart. Local `local.retentionDays` defaults to seven; fresh
submissions apply the current setting, while exact retries preserve expiry.

For isolated production-user tests, start `diffx-server` with a temporary
persistent database and bootstrap username/password. Login and create API tokens
through Settings, which works before any upload. Signup activates accounts without
a restart when enabled. Operator commands require stopping the database owner.
Credentials scope REST, MCP, jobs and events. Local mode automatically supplies
its stable owner and exposes no account UI or management routes. Do not provision
against the running personal service.

## Server development

Build both binaries with `mise run build`, then run server mode directly:

```sh
DIFFX_SERVER_USERNAME=alice DIFFX_SERVER_PASSWORD=… \
  DIFFX_SERVER_INSECURE_COOKIES=true DIFFX_SERVER_STATE=./test-data/state.db ./dist/diffx-server
```

Or use the checked-in Compose setup:

```sh
DIFFX_SERVER_USERNAME=alice DIFFX_SERVER_PASSWORD=… mise run docker:up
docker compose logs server
mise run docker:down
```

The same server binary handles authentication, durable admission, workers and
queries in both setups. The Docker tasks stage embedded web assets before
building the image.

| Setting                         | Binary default  | Container default | Flag                    |
| ------------------------------- | --------------- | ----------------- | ----------------------- |
| `DIFFX_SERVER_HOST`             | `127.0.0.1`     | `0.0.0.0`         | `--listen HOST:PORT`    |
| `DIFFX_SERVER_PORT`             | `7980`          | `7980`            | `--listen HOST:PORT`    |
| `DIFFX_SERVER_STATE`            | `data/state.db` | `/data/state.db`  | `--state PATH`          |
| `DIFFX_SERVER_RETENTION_DAYS`   | `7`             | `7`               | `--retention-days DAYS` |
| `DIFFX_SERVER_USERNAME`         | Unset           | Unset             | `--username NAME`       |
| `DIFFX_SERVER_PASSWORD`         | Unset           | Unset             | None                    |
| `DIFFX_SERVER_SIGNUP_ENABLED`   | `false`         | `false`           | `--signup-enabled`      |
| `DIFFX_SERVER_INSECURE_COOKIES` | `false`         | Compose: `true`   | `--insecure-cookies`    |

The server listens on 7980 so a local `diffx [PATH]` (7981–7990) and the server
run side by side. The `DIFFX_SERVER_` prefix keeps deployment variables from
configuring the CLI, and the CLI's `DIFFX_LOCAL_*`/`DIFFX_REMOTE_*` from
configuring the server.

Bootstrap requires both username and password. It creates the first account once
and never replaces passwords on restart. No default credentials or admin-token
file is generated. Signup remains disabled unless explicitly enabled. `memory`
and `:memory:` disable persistence.

Sign in with username/password and create a named token in Settings. Use the
`collector` preset for producers, or `api` for review APIs/MCP. Token secrets are
shown once; expiry defaults to 90 days (selectable 1–365). Tokens cannot manage
accounts. Browser sessions are seven-day opaque cookies with CSRF protection on
unsafe requests. Secure cookies are the binary/image default; explicitly enable
insecure cookies only for HTTP development. Production requires HTTPS.

With the server stopped, operator commands read passwords from stdin:

```sh
diffx-server user create --username bob --state ./test-data/state.db --password-stdin < /path/to/private-password
diffx-server user reset-password --username alice --state ./test-data/state.db --password-stdin < /path/to/private-password
```

Password reset revokes browser sessions. Restart the server after provisioning.

Precedence is defaults → explicit JSON file → env vars → explicit flags. Use
`--config-file FILE` or `DIFFX_SERVER_CONFIG` to select a server config. The file
supports `host`, `port`, `state`, `username`, `retentionDays`, `signupEnabled` and
`insecureCookies`. Bootstrap passwords are supplied through env vars. The file
must exist and
can be mounted read-only; startup never creates it or a config lock. Personal
CLI config, `DIFFX_RUNTIME_DIR`, producer URL settings and external web assets
do not configure server mode. Restart to apply changes.

Compose keeps the SQLite database, sidecar files, ownership lock and credential
hashes in the `diffx-data` volume. The root filesystem is read-only; the
image prepares `/data` for UID/GID 10001. If using a bind mount instead, make
the directory writable by that identity. Keep database files inside `/data`.
`mise run docker:down` preserves the volume; `docker compose down --volumes`
explicitly deletes it. Stop the server and back up the whole directory before
starting with a fresh database; a volume is not a backup.

Set `DIFFX_SERVER_PORT=7979 mise run docker:up` for a different published host
port; the container always listens on 7980. Compose also accepts
bootstrap username/password, retention, signup and insecure-cookie overrides,
from the shell or a git-ignored `.env` beside `compose.yaml`. Compose defaults to
insecure cookies because its HTTP listener is published on loopback; explicitly
set `DIFFX_SERVER_INSECURE_COOKIES=false` behind production HTTPS. The fixed
`diffx` project name keeps one
volume regardless of the checkout directory.
For other settings, use image env overrides or a Compose override file.
Only one server may own a SQLite database; competing starts fail rather than
share ownership. SIGTERM stops HTTP and workers before releasing storage;
unfinished admitted jobs recover from the durable queue after leases expire.

This setup targets one server with SQLite. PostgreSQL, multiple replicas,
browser login and integrations remain separate future changes.

## Build pipeline

The production build generates API types, builds the web application, stages the
Vite output under `internal/webui`, and embeds those assets into the Go binary.
The resulting `dist/diffx` and `dist/diffx-server` executables have no
Node runtime dependency. Git is required only for producer-side checkout
collection. Local/remote server queries read SQLite and never invoke Git.

The OpenAPI contract in `packages/api/openapi.yaml` is the frontend/server
boundary. After changing it, run:

```sh
mise run generate
```

## Frontend conventions

Follow [DESIGN.md](../DESIGN.md). Semantic tokens in `apps/web/src/tokens.css`
map into Tailwind and shadcn theme roles. Reuse primitives from
`apps/web/src/components/ui`; keep authored CSS for specialized layout, dynamic
geometry, and Pierre's measured rendering boundary.

`App.tsx` composes page sections. `AppProvider` composes appearance, sidebar,
and workspace owners. The identity owner loads server mode and the current
account before mounting review state; distributed login and `/settings` work with
zero observations. Account changes dispose workspace requests and event streams.
Local mode bypasses account flows. Consumers subscribe through domain hooks for diff source,
navigation, draft, review, reviewed files, and collapse state. Collapsed diff
files (per stream and scope) and closed tree folders (per stream) persist in
`sessionStorage`, so reloads and newer snapshots keep them within a tab; piped
snapshots key by context. One draft persists
across scopes. Context selection uses the Repos view and the popup picker (Cmd/Ctrl+K
or the Repos “Search reviews” magnifier). Repos and Diff share the panel
beside the activity rail; selecting a stream loads its diff and switches to the
file tree. Clicking the active view toggles the panel. `use-sidebar.ts` owns
view selection, visibility, shared width and mobile drawer focus.
`Navigation.tsx` owns the common frame and resize handle for both views. Compact header
controls remain available when the rail is hidden; the header shows the open
review's identity. The selected context scopes every request. The session owner exposes
local checkout sync when bootstrap reports `collect`; loading `/` or a live
`/streams/{streamId}` URL, the header's “Sync all checkouts” icon and Alt+R call
`POST /api/v2/collections`. Load-time sync runs in the background while the
first available stored head opens: the requested stream's head, or the most
recently active changed one for `/`. Collection completion never replaces an
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
action opens the most recently active unopened changed head. The drawer and
header share one per-browser seen-state owner; first visits baseline existing
heads and unchanged submissions do not create unread prompts. Activity ordering
uses catalog `lastChangedAt`, falling back to captured collection time, with
stable stream/context identity ties. Repositories use their most active visible
stream. Startup and root selection follow the same ordering. Collection triggers
are unchanged; no periodic collection or Git watcher is scheduled.
The “Switch review” picker first selects a repository
or Piped, then an immutable observation. Repository observations retain branch,
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
The compact header keeps Theme at its trailing edge and exposes no snapshot
delete action. Snapshot deletion through the API removes its stored diff scopes,
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
across writes or owner disposal. Keep section-only state within its component.
Scrolling the diff selects the top visible file in navigation; selection advances
only after that file’s entire diff leaves the viewport. Scroll-driven selection
reveals its tree row without expanding the diff or navigating again.

See [architecture.md](architecture.md) for repository boundaries and dependency
rules.

Shared producer orchestration in `cmd/diffx` resolves config → environment →
flags once, pins verified retries to account/database identity and serves manual/hook
delivery. Hooks use session-scoped acknowledgements plus server reconciliation;
their private pending data retains a separate seven-day expiry. Token rotation
preserves a verified destination's pending data; offline captures with unknown
owners remain bound to their original credential until authenticated. REST and MCP
compose `reviewservice`; global `/mcp` provides catalog discovery and stored
diff/patch retrieval with explicit context IDs.

See [plugins.md](plugins.md) for native harness installation, asynchronous
checkout sync, configuration precedence and hook diagnostics. Normal builds and
checks compile the adapters; host SDK dependencies are development types only.
Pi and OpenCode's self-contained `dist/index.js` artifacts are committed so a
clone is ready to install without a build. After changing adapter source, run
`mise run plugins:build` and commit the regenerated artifacts. Codex and Claude
load their checked-in hook definitions through the root marketplace manifests.
