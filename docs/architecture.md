# Architecture

Diffx has one stored-data application with two compositions. This is the
canonical record of package boundaries and architectural decisions.
[System semantics](system.md) covers identities and durability;
[development](development.md) covers commands and checks.

The MyGo desktop is the default client of the personal SQLite composition,
including its explicit TLS network access policy. `apps/desktop` is a separate
Go module with a Vite entry,
consuming public REST/SSE and local-session wire contracts. It never owns
SQLite, Git collection or review rules, and cannot import root `internal/`.
Its thin `app` package owns MyGo, OS menus and dialogs; `session` alone executes
the bundled matching CLI; `host` serves embedded assets and proxies allowlisted
public REST/SSE on one loopback origin. Private control endpoints stay private.
Browser (`apps/web`) and desktop entries mount `ReviewApplication({ platform })`
from `packages/review-app`, sharing views, state, styles, workers and Pierre
geometry. `packages/ui` remains the design-system owner. PWA activation requires
the browser entry and remote bootstrap. Desktop has no parallel Go diff widgets,
token generator or preferences implementation. Web preferences start fresh and
persist across restarts through a stable desktop loopback origin; server
review data and public local-session ownership rules remain unchanged.

## Local validation

`tools/check-runner` owns immutable source capture, group selection, successful
validation records, reporting and Docker Compose orchestration. Mise owns the underlying
commands. Tests and stateful checks run in private Linux x64 containers. Static
checks run on the host against a private temporary source/dependency snapshot,
without requiring Docker. Hosted workflows publish stable releases; they do not
run PR validation or publish development builds.
A PR gate validates clean committed HEAD,
whereas interactive checks snapshot working files without certifying HEAD.

Preparation and build outputs are immutable reusable inputs. Every executing
group owns its writable files and temporary state. Container groups own their test services. Browser clients
and fixture servers share the group's container; application ports are never
published to the host. Postgres is supplied by a private service in the
group's own Compose project. Existing per-test database isolation still applies
within each group.

Successful results are reusable only for matching source, configuration,
toolchain and runner inputs. Commit identity records provenance but is not a
cache key. Host static results also include the installed Go, Node and pnpm
binary digests. Unknown inputs invalidate conservatively; failed and cancelled runs
never become successful validation records. Generated-file checks compare
explicit generator outputs with the submitted baseline.

Application binaries and web build outputs are never committed. The host static
group includes `mise run check:artifacts` to reject tracked build outputs.
Built plugin distributions and generated API/SQL source remain committed.
Stable publishing tags `main` directly and stages embedded web assets only in
the workflow workspace and creates no asset commits. The stable CLI shell
installer distributes `diffx`; the separate desktop installer exposes a matching
`diffx` and `diffx-desktop` pair. `install-dev.sh` builds unreleased branches or
commits locally through each checkout's `install:source` mise task. Go source
installs are unsupported because source refs lack embedded assets. See
[release.md](release.md).

One multi-stage check image serves every worktree through the Docker build
cache; identical toolchain, dependency and source inputs reuse the same layers.
Each run owns resources named by its admission slot and removes them on
success, failure and cancellation; the next holder of a slot removes anything a
dead run left. Shared Go build and pnpm caches are content-addressed and safe
across concurrent runs. Broad pruning is forbidden.

## Modes are compositions

| Concern    | Local `diffx [PATH]`                                                       | Remote `diffx-server`                    |
| ---------- | -------------------------------------------------------------------------- | ---------------------------------------- |
| Collection | Selected checkout(s) at start, live page loads and sync; stdin once        | Separate finite sync or hook producer    |
| Admission  | Direct application call; startup runs after serving                        | Authenticated durable queue and worker   |
| Lifetime   | Desktop-owned by default; explicit foreground modes; singleton replacement | Foreground; graceful worker shutdown     |
| Storage    | One owner of persistent SQLite                                             | Shared Postgres; stateless processes     |
| Queries    | Selected checkout(s) or capture via REST/MCP                               | Same application services via REST/MCP   |
| Access     | Owner-only socket; optional loopback viewer; explicit pinned TLS policy    | Account credentials wrap the application |
| Identity   | Automatic stable local owner; no account UI                                | Account sessions and scoped API tokens   |

`diffx remote [PATH]` selects the personal SQLite composition with explicit
network access; `diffx-server` remains the distributed Postgres composition.
Personal pairing does not configure distributed `remote.url` or `remote.token`.

Source support and review policy resolve snapshot capabilities. A mode never
invents contents, scopes or refresh support. Queue presence at the composition
root enables remote admission; no queue means no admission routes.
Captured comparison kinds describe endpoints independently of the producer and
deployment mode. Collection owns Git endpoints; ingestion owns semantic
validation and legacy interpretation; context queries expose stored scope
summaries. Selecting a comparison reads the same immutable observation and never
triggers collection or resolves the latest stream head.
Manual sync and hook producers collect only the selected checkout. A local
process started on one checkout path does the same unless `--scan-worktrees`
adds that repository's registered worktrees. A local process given `--path` or
a directory outside a repository discovers every repository under its paths
(bounded depth, stopping at repositories, skipping hidden directories) and
collects all of their registered worktrees, because it serves a developer's
whole workspace. The entry point resolves this scope once at startup
(`collector.Workspaces`) and reports it in the bootstrap as `input: "checkout"`
or `"checkouts"`. Stored observations cannot refresh from Git. A local checkout process
discovers and collects again only on user request (`POST /api/v2/collections`):
opening a live URL or pressing Sync. Discovery feeds checkouts directly to four
collection workers through a bounded pipeline. Each finished checkout publishes
immediately through one serial publication owner, while slower Git reads continue.
Each observation commits independently; one checkout failure is reported without
blocking the others. Cancellation stops Git work and joins every worker before
collection returns.
Synced snapshots never replace the open review, matching remote arrivals. Piped
and fixture processes have no collection route. Local queries are scoped to the
discovered checkouts, or to the capture's submission, because the persistent
home-directory database retains every earlier run. The entry point supplies an
initially empty selection and widens it immediately before publishing each
collected checkout; `contextservice` applies it to listing, counting, resolution and deletion, and out-of-scope
contexts are not found. A scoped selection without checkouts matches nothing.
Local listeners and the browser start before checkout discovery and collection. A server-owned task performs the
initial collection; the first live-page collection request joins its result,
including when the task finished before the browser opened. Explicit Sync always
collects fresh snapshots. Collection failures, queue storage errors and failed
retention pruning are logged and leave the server running for retry,
and shutdown cancels and joins collection before releasing database ownership.

Activity metadata is collected by the shared producer pipeline, never by catalog
reads. Both compositions project activity from the winning stream submission and
use it for current-work navigation; collection time and acceptance sequence still
choose the stream head. Workspace discovery and web-triggered collection remain
local-only. There is no watcher or periodic collection.

A local instance serves the application and its private control API on one
owner-only Unix socket (`diffx.sock` in the 0700 runtime directory, mode 0600).
Filesystem permissions and a peer user check (`SO_PEERCRED`/`LOCAL_PEERCRED`)
are its authority; there is no lifecycle token or descriptor file. Only the
holder of the ownership lock binds the socket, so whoever answers there is the
running instance, and a socket file left by a crash is replaced under that
lock. Control supports status, shutdown and an instance-bound lifetime stream,
and is mounted only on the socket.
Replacement uses lifecycle locking and graceful shutdown; a PID alone is never
authority. Browsers cannot reach a Unix socket. Default `diffx [PATH]` hands
its input to desktop over owner-only IPC and returns after desktop acceptance.
The desktop session serves only the socket unless an explicit `--port` requests
a separate loopback viewer. `--browser` selects a foreground viewer and opens
it; `--no-browser` selects a foreground viewer without opening a client;
`--headless` selects socket-only foreground serving. Failure before desktop
acceptance permits foreground browser fallback. Later server/connection failure
stays visible in desktop. Local viewer ports and desktop/browser gateway origins
remain loopback, with matching Origin/Host and cross-site request checks.
`diffx status --json` reports the socket and optional viewer.
`diffx dev --fixture FILE` is isolated from personal lifecycle discovery and
serves only a viewer.

`diffx remote [PATH]` stays foreground and adds a TLS listener, default `:7981`,
with explicit `--listen` and optional `--advertise HOST:PORT`. It uses the same
SQLite/runtime singleton and never silently promotes an existing local process.
Without explicit advertisement, a concrete bind address is used; wildcard binds
prefer the SSH session's server IP, otherwise enumerate non-loopback interface
addresses. There is no external discovery. `internal/pairing` generates a fresh
TLS identity per run and a 256-bit one-use invitation expiring after ten minutes.
`packages/connection` owns the `diffx1_` encoded invitation and native Unix,
loopback TCP and pinned TLS transports. Pairing atomically exchanges the invitation
for a run-scoped credential, with bounded attempts and only credential hashes in
server memory. `diffx remote pair` rotates the unused invitation through private
socket control without invalidating existing credentials. Restart requires a new
invitation. The CLI caches credentials under its private state directory in `connections/`
(owner-only directory and files), separately from distributed configuration, so the original connect command can reconnect
during the same run. Credentials never enter React, browser URLs or child argv.
The TLS listener serves pairing and the shared `api.PublicReviewRoute` allowlist,
including checkout collection and review REST/SSE; it excludes private control,
MCP, account and ingestion routes. Native clients pin the certificate, require
TLS 1.3 and never fall back to plaintext.

Desktop session composition reuses the same local application and database
owner. Private CLI entries emit `packages/localprotocol` readiness with a native
connection target and optional viewer, and retain a parent-lifetime pipe. A
compatible existing canonical scope can be attached; another scope requires
explicit replacement. Attachment never transfers ownership. Closing the pipe
cancels and joins only an owned server. One desktop window focuses the same target;
changing workspace or connection requires confirmation. Cancel preserves the
current session. Accepted transitions cancel and join prior requests and streams.
The host proxies public REST/SSE directly through native Unix/TLS/SSH transports
on its stable loopback origin. Remote leases never own the server. Reconnect
backoff caps at 15 seconds for two minutes, then offers Retry; mutations are never
automatically replayed. `connect --browser`, `--no-browser` and `--json` retain
foreground lifetimes through a filtered loopback gateway. SSH uses system SSH
configuration and socket forwarding, with viewer forwarding when socket
forwarding is refused. The public gateway excludes private controls; a raw SSH
socket forward still carries them.

Independent local sessions use distinct `DIFFX_RUNTIME_DIR` directories, which
give each its own socket, and viewer ports (`--port 0` selects a free port).
Socket paths are limited to 103 bytes on macOS and 107 on Linux, so runtime
directories must stay short. The runtime directory owns lifecycle
discovery and default config/state paths; explicit config or state overrides can
still point sessions at shared state. Each SQLite database retains one process
owner. See [development.md](development.md) for isolated checkout commands.

## Modules own behavior

- `review`: data, source contract and pure comment/patch rules.
- `collector`, `diffsource`: Git, discovery, parsing and finite collection.
- `diagnostics`: bounded producer reports, identifier redaction and private
  report storage/export. Composition roots connect consumer-owned observers;
  collection and Git adapters do not depend on report persistence.
- `ingestion`: versioned request/job contracts, validation and HTTP client.
- `contextservice`: owner-scoped publication, resolution and catalog.
- `reviewservice`: shared snapshot reads, comments, marks and capability checks.
- `reviewdata`: storage-independent identities, results and errors.
- `reviewsession`: a resolved review and its source/policy capabilities.
- `identityservice`: account, sign-in method and audience (app members or
  console admins), password, reset link, session, API-token, admin role,
  account administration, invite, app admission and Google sign-in policies.
- `google`: Google OpenID Connect wire adapter: authorization URL with PKCE and
  nonce, code exchange and ID token claim checks. No storage or diffx policy.
- `github`: GitHub wire adapter: app JWT, installation tokens, OAuth web flow,
  REST reads, webhook signatures and payloads. No storage or diffx policy.
- `githubsync`: GitHub integration rules: which accounts may follow which
  repositories, sealed user tokens, webhook deduplication, coalesced pull
  request refresh intent, reconciliation, fetch leases, server-authored
  pull request observations, which snapshots notify followers, and public
  pull request previews (public demand, ordered read sources and their
  fallback rules, refusal of private, oversized or restricted repositories,
  refresh rules and status) owned by the reserved
  `github-public` owner. Remote composition only.
- `webpush`: Web Push wire adapter: VAPID keys and JWTs, RFC 8291 payload
  encryption and push service response classes. No storage or diffx policy.
- `pushservice`: push enablement, VAPID key load-or-create, account device
  subscriptions and the outbox delivery worker, whose leased claims let
  replicas share delivery. Remote composition only.
- `fetchprotocol`: lease/result contract and client between `diffx-server`'s
  internal listener and `diffx-fetcher`.
- `fetcher`: `diffx-fetcher`'s partial-clone mirrors and offline collection of
  leased pull request heads. It holds no account, GitHub App key or store.
- `reviewstore`: local SQLite transactions, stored reads and retention.
- `pgstore`: the server's Postgres persistence: goose migrations, sqlc queries,
  every remote storage contract (content, reviews, identity, GitHub, push,
  queue), the locks that keep replicas correct, including the advisory
  try-locks that run retention pruning and GitHub startup reconciliation on
  one replica at a time, and the `LISTEN/NOTIFY` channels that keep replicas
  informed. Remote composition only.
- `ingestionqueue`: admission/lease contract and worker failure policy; jobs fail
  permanently only for `review.Permanent` rejections (4xx except 408/429).
- `submission`: destination identity, delivery and the acknowledgement TTL,
  shared by manual sync and hooks; owns manual sync's route-scoped outbox
  recovery (`Sync`). Hooks keep job- and session-scoped capture and
  acknowledgement state because their suppression scope differs. No dependency
  on hook scheduling.
- `hooks`: finite scheduling, coalescing and session-aware suppression.
- `httpapi`, `mcpapi`: transport parsing, projection and error mapping. Both
  reach review operations through `reviewservice.Catalog` and hold no store.
  `mcpapi` also owns the stdio bridge (`diffx mcp`), which relays MCP messages
  unchanged to an HTTP endpoint its caller dials, here the local socket.
- `serverapp`: transport/task composition; `localserver`: local lifecycle;
  `controlapi`: its private control API, credential-free because composition
  mounts it only on the owner's socket; `unixsocket`: socket bind, dial and
  peer credentials, with admission policy left to the listener's composition;
  `pairing`: run-scoped TLS identity, invitations, credentials and network access
  policy; `clientgateway`: filtered browser access through native connections;
  `desktoplaunch`: CLI desktop discovery, matching-pair checks and accepted handoff;
  `sshforward`: native SSH forwarding, which runs the system `ssh` to read a remote
  `diffx status --json` and forward its socket, or its viewer port when the SSH
  server refuses socket forwarding, to a local loopback port; it holds no
  credentials and serves nothing itself. A forwarded socket carries its
  control API too, so any local process that can reach the forwarded port can
  stop the remote instance; this is accepted alongside the existing read
  exposure of loopback ports, and browsers stay refused. Desktop and browser
  gateways expose only public review routes; `remoteserver`: distributed
  member authentication and worker composition; `authhttp`: browser
  authentication HTTP shared by remote transports (session cookie, CSRF,
  problem and identity error mapping, sign-in admission and rate limiting,
  deployment probes); `consoleserver`: the admin console transport and
  composition (`diffx-console`), which composes identity for the console
  audience over the server's store and runs no workers or review services.
- `config`, `processlock`, `browser`, `version`: configuration and private state
  paths (`StateDir`, honouring `DIFFX_RUNTIME_DIR`), process ownership,
  browser launch and build metadata; `processmetrics`: server process metrics;
  `webui`, `consoleui`: embedded app and console assets. `testsupport` is test-only. `storetest` is the
  test-only behavioral contract every review store runs for shared content and
  review operations; identity, stream and patch digest rules live in
  `ingestion` and `review`, so stores never derive them independently.

Interfaces live beside consumers and require the needed operation. Optional
interface assertions must not change application behavior. Storage contracts
describe atomic application operations, not independently committed CRUD steps.

Local composition resolves its stable owner automatically and exposes no account
management routes. Remote composition resolves sessions or API tokens to opaque
account IDs before application access. Review operations receive an owner without
mode branches. `remoteserver` activates owner-scoped services on demand, including
for accounts created after startup; requests, events and workers share that owner
registry and its shutdown lifetime; disabling or deleting an account stops its
services and event streams. Replicas share no memory, so `pgstore` sends a
`NOTIFY` in the transaction that ingests or deletes a context, changes a
stream's comments, replies or resolutions, disables or deletes an account,
changes app settings, or queues an ingestion job; a rollback sends none. Each
replica's dedicated listening session relays every commit, its own included,
to that owner's subscribers, removes the account's services, reloads the
admission settings snapshot or wakes the worker.
Remote services therefore skip in-process publication
(`contextservice.ExternalEvents`); local composition keeps it. `reviewservice`
reports committed comment changes to the context provider it was resolved
through (`CommentEvents`), which publishes them only in local composition. Notifications
are hints: after a lost session the listener reconnects with backoff, asks
every active owner's clients to reload and reloads settings, and the worker
keeps polling.
Identity service owns credential, administration and admission rules,
including sign-in policy, admin roles and the last-admin guard; each entry
point composes its audience (members for `diffx-server`, admins for the
console), and transports own cookies, CSRF and route permissions. Admission
settings are database state, not configuration: `pgstore` owns the settings
row, its notification and the in-transaction check (`FOR SHARE`) that account
creation through open signup, password signup and password invite acceptance
repeats; `remoteserver` loads a snapshot before serving and composes its
reload task, keeping the last snapshot when a reload fails; the console reads
and writes the row directly, uncached. `diffx-server` serves no admin routes.

The frontend identity owner loads the explicit server mode before mounting review
state. Distributed login and settings work without any observations. Account
changes dispose requests, event streams and workspace state. Local composition
opens reviews directly without exposing identity controls. Only remote mode
links the PWA manifest and registers the service worker, because local ports
change between runs and installs would orphan per origin.

## Principles and limits

1. Share semantics, not coincidental syntax. DRY gives identity, authorization,
   replay and review rules one owner. Local direct ingestion and remote queue
   admission have different failure contracts and remain distinct.
2. Separate collection from queries. Reads survive checkout removal without Git.
   Failed collection never becomes a successful empty observation.
3. Compose policy at entry points. Keep mode branches out of domain operations;
   prefer small explicit policies over a generic feature framework.
4. Immutable content, mutable review state. Fresh matching submissions can reuse
   review state and extend retention. Exact retries cannot change retention or
   resurrect deleted content. Identity commits with the observation.
5. Authorize before resource access. Owner/context/scope checks apply equally to
   REST, MCP, historical versions, mutations and job polling.
6. Acceptance is not completion. Remote 202 means durable admission; succeeded
   means committed publication. Retries preserve payload and submission ID.
7. Cancellation and ownership are contracts. Stop workers and serving before
   releasing database ownership. Uncertain delivery stays pinned to its target.
8. Generate wire types; share examples for cross-language rules. TypeScript
   aliases OpenAPI types. Real HTTP responses need runtime schema checks.
9. Prefer boundary tests; keep valuable unit tests. Parsing, hashing, leases,
   retry policy and UI recovery have independent failure modes. Delete obsolete
   behavior tests and implementation-shape assertions, not useful small tests.
10. Add complexity for an observed requirement. No REST cache for cheap immutable
    database reads, background review daemon, or speculative infrastructure.

## Contracts and evolution

Collection uses bounded patch streams and immutable object batches rather than
one Git process per file. Raw diff modes and object IDs stay private to the Git
adapter. The collector owns preview allocation and retains the existing ordering,
per-file limits and final mutable checkout verification. Stored review sources
keep their scalar read contract. Blob reuse lasts one collection and retains at
most 16 MiB; subprocess admission and process-tree cleanup apply to batch reads.

Local startup/Retry, manual sync and hooks record request-scoped stage and command
metadata. Failed attempts persist schema-1 diagnostic JSON reports automatically;
`--debug` also retains successful reports. Reports contain no captured content,
raw argv, environment or exception text. Paths, refs and URLs are pseudonymized
with a report-local key. Report references are additive API failure metadata and
never enter observation identity or ingestion retry payloads. Export is an
explicit local CLI operation. Report persistence failures cannot change a
collection's result, and cancellation/cleanup failures remain separate from its
primary failure.

`mise run test:contracts` runs production dependency checks, frontend token
guards, shared Go/TypeScript comment examples, the Go suite (including
storage/lifecycle/queue/transport boundary tests) and real HTTP schema
validation. Local `mise run check` and explicit `check:pr` cover these checks
alongside web unit/DOM/browser, plugin and docs-site suites. Required check groups
either execute on their declared host/container executor or reuse matching successful validation.
Up to three groups run concurrently, each container limited to two CPUs;
Chromium uses one worker and Go parallelism is bounded. Two participating
invocations may run concurrently through user-level admission. The runner owns
cancellation and Compose project cleanup before releasing admission; a failed
group lets the others finish and fails the invocation. Immutable preparation and
build outputs are reusable. Public mise test tasks use Compose; package.json test scripts and hidden
`exec` mise tasks are the raw commands groups run inside containers.
`test:contracts:rules` holds only the non-Go guards, because aggregate checks run every Go
package; no hand-maintained package list can miss a new boundary test. All static
checks, generated-file verification and test suites stay local. Publishing retains
the builds and checksums needed to produce release artifacts.

`mise run check:boundaries` classifies every package under `cmd/` and `internal/`
using `tools/check-boundaries`. Unclassified packages, forbidden dependencies and
production imports of `testsupport` fail. Process execution and SQL access stay
with their explicit owners. The checker inspects production imports for the
current build platform; tests may import adapters to exercise their contracts.

The same guard classifies public Go packages under `packages/` and the separate
desktop module. The generated
Go REST client and local readiness DTOs depend on wire concerns only, never
server internals. Root Go packages cannot import MyGo: the webview shell belongs to the
separate desktop application module. OpenAPI remains the source of REST wire
types for both TypeScript and Go; named components share capabilities without
changing the JSON contract.

The `desktop` check group runs host/bridge/session tests and production-tag
tests in its own Linux container. Its source identity includes shared React
source, desktop entry and build configuration, nested Go manifests and framework
pin. GTK/WebKitGTK and fonts belong only to desktop preparation. Static
architecture and formatting guards remain on the host with private snapshots.

The required `desktop-window` group runs actual webviews under Xvfb/D-Bus,
covering workers, rendering, review interactions, clipboard and local-server
lifecycle. Only its test binary enables automation. `desktop-package` compiles
and inspects all four macOS/Linux archives, including embedded desktop/browser
frontend bytes, matching server versions, notices and inspector exclusion.
Actual macOS acceptance requires the explicit `desktop:platform-smoke` on a Mac.
Linux containers cannot certify macOS runtime behavior. See
[desktop-webview.md](desktop-webview.md).

## Making changes

Before implementation, identify the behavior's owner, consuming contract and
invariants from this document and [system.md](system.md). Put shared rules in
their existing owner and compose delivery/lifecycle differences at entry points.
For shared changes, verify both compositions through existing boundary tests;
add a regression test when coverage is missing. Check semantic outcomes rather
than private implementation shape.

The PR template records ownership, contract/mode implications, validation and
intentional design changes. Change a boundary policy only for a concrete new
responsibility, updating its rationale and tests in the same PR. Passing checks
does not establish cohesive modules, correct DRY abstractions or visual quality;
those remain review responsibilities. `main` requires no hosted validation status.
Architecture/design guards and full-suite validation run locally through explicit
`mise run check:pr` and are recorded in the PR evidence. Pushes do not trigger
the full suite, and local validation is not remote enforcement.

## Adapter and schema evolution

`packages/buildmeta` owns desktop package identity records shared by the CLI and
independent desktop module. Packaged executables use the retained role/version/
source record for runtime version reporting; cross-target verification inspects
that same record. Go omits linker settings with `-trimpath`, so linker-flag output
cannot establish a matching pair. Standalone CLI publishing keeps its existing
version metadata, and development pairing compares explicit build labels.

New adapters must preserve ownership, atomic publication, retention, replay,
lease fencing and commit-before-ack recovery. External brokers require atomic
scheduling intent (an outbox). The server and local mode do not share a storage
adapter: `reviewstore` (SQLite, local) and `pgstore` (Postgres, server) each
implement the consuming packages' contracts, so server features may use Postgres
directly (row locks, `SKIP LOCKED`, `LISTEN/NOTIFY`, extensions) without a
lowest-common-denominator interface. Shared content and review semantics are
held equal by `storetest`, which both stores run; identity rules live in
`ingestion` and `review`. SQLite retains one owning process.

Server schema changes are goose migrations embedded in `diffx-server`
(`internal/pgstore/migrations`). Every replica migrates at startup under an
advisory lock; a binary never downgrades a newer schema. Migrations are
expand/contract: each release must keep the previous release working against
its schema, because replicas upgrade one at a time. A destructive change is
split across two releases. A migration that cannot keep the previous release
working raises `schema_compatibility.floor` to its own version, so older
binaries refuse the database instead of corrupting it; migration 10 raises it
to 10 because it drops `account_invites.admin`, which the previous release
reads, so that upgrade is not rolling; migration 12 raises it to 12 because it
drops `remote_accounts.username`. The server requires
PostgreSQL 18, never resets its database, and keeps its tables in the `diffx`
schema. Server state from earlier SQLite volumes is not imported; an upgraded
server starts with an empty database.

Only public v2 routes and ingestion protocol 4 remain. Retired: `review`,
`service`, `serve`, `capture`, background daemon startup, public v1,
synchronous ingestion and legacy capture/worktree storage.

Diffx starts with fresh configuration, state and producer identities under its
own directories. There are no aliases or migration paths. Ingestion protocol 4
uses `X-Diffx-State` to pin submission and polling to the destination identity;
older protocols are rejected. Release artifacts use the explicit `diffx` project
name, independent of the checkout directory.

Local SQLite schema 17 preserves current state across starts. Schema 17 added
`comment_replies`, which cascades with its comment. Schema 16 dropped the
server-only tables earlier schemas created in every local database
(accounts, sessions, tokens, administration, Google sign-in, invites, GitHub
integration, push notifications and the `ingestion_jobs` queue), because server
state lives in Postgres; `ingestion_records` remains the local retry identity.
Schema 14 added captured branch commit tables (`observation_commits`,
`observation_commit_ranges`). Schema 11 moved review state from each snapshot
scope to its stream and scope, because comments and reviewed marks must survive
new collections and snapshot deletion. Schema 11 and later upgrade in place:
later schemas only add tables and indexes or drop tables that are provably
always empty in local state, so opening runs the idempotent statements and
records the new version in one transaction, because local state must gain
commit tables and shed server tables without a reset. A later schema may drop a
table only when local state never writes it; the additive floor stays 11. A
schema change that alters existing tables or drops local data needs a new
compatibility floor instead. Older-than-11, future and unrecognized schemas are
refused; opening a database never resets it. `reviewstore.Reset` is the only reset: under the
ownership lock it moves current or older diffx state and its SQLite sidecars to
a timestamped backup beside the database, keeping only the newest backup, and
refuses newer or unrecognized files. Only local composition decides to call it,
because local state is one developer's re-collectable workspace: `diffx` asks on
a terminal for older state, and `--reset-state` (also on `diffx dev`) consents
up front for any wipe-safe state. `diffx-server` never opens SQLite state and
never resets its Postgres database. State errors identify the
database and backup directory, name the compatible schema, and show the
`--state` override for explicitly selecting fresh state; local refusals of older
state also suggest `--reset-state`. Fresh state has no saved reviews.
Install the server first when adding optional protocol 4 metadata: older servers
reject unknown request fields and new diff scopes. Captured comparison kinds,
the `uncommitted` and `committed` scopes, activity, pull request metadata and
captured `commits` with `commitsTotal` retain protocol 4. Public ingestion rejects the `github` source, the `github`
trigger and `pullRequest` metadata at the transport, because only the server's
GitHub integration may claim a hosted pull request; shared validation accepts
them for that internal admission path.
Existing saved submissions remain replayable and schema 11 state upgrades without reset.
Legacy comparison kinds are interpreted on read; stored payloads and their retry
identities are not rewritten. Upgrade collectors after the server.
Versioned producer directories ignore old manual/hook pending payloads.
Stop old binaries before upgrading.

Remote configuration is owned by `config` and composed by `cmd/diffx-server`:
defaults, an explicitly selected read-only server JSON file, `DIFFX_SERVER_*`
environment, then explicit flags. Server startup does not discover, create or lock
personal CLI config. The binary defaults to loopback port 7980; the container
supplies overridable env defaults for its listener. `DIFFX_SERVER_DATABASE_URL`
selects the Postgres database holding all server state, credentials and the
durable queue. It holds credentials, so it is read only from the environment,
never a file or flag, and diagnostics never echo it. App admission lives in the
database, not configuration: startup refuses the removed `DIFFX_SERVER_SIGNUP`
and `DIFFX_SERVER_PASSWORD_SIGNIN` and file keys `signup` and
`passwordSignIn`, naming the console setting that replaces each.
`DIFFX_SERVER_AUTH_RATE_LIMIT` (default off) and `DIFFX_SERVER_CLIENT_IP_HEADER`
are also environment only, because trusting a client IP header is a property of
the deployment's ingress, not of a portable config file. `authhttp` owns the
limit and the header parsing; `pgstore` owns the atomic per-client fixed-window
counter in an UNLOGGED table, which replicas share and which loses only counts
on a crash. Only remote composition serves `/healthz` and `/readyz`; local mode
has no deployment probes.

`cmd/diffx-console` composes the admin console from `DIFFX_CONSOLE_*`
environment only: its own listener (loopback 7970), the server's database URL,
its origin, the app's origin for invite and member reset links, and its own
Google client whose callback is on the console origin. It migrates through
`pgstore` under the same advisory lock as the server, so either binary may start
first. Account and admission settings changes reach server replicas through
the store's `diffx_account` and `diffx_settings` notifications, not through any
call between the binaries. The
console has its own OpenAPI contract (`packages/console-api`, `/api/v1/*`) and
app (`apps/console`); it shares UI primitives through `packages/ui`, never app
code or the server contract.

Personal CLI configuration keeps modes in separate namespaces so one machine runs
both compositions at once: `local.*` and `DIFFX_LOCAL_*` configure only
`diffx [PATH]` and personal serving (optional viewer ports 7981–7990), `remote.*` and `DIFFX_REMOTE_*` only sync and
hooks. Mode is chosen by the command, never by a config field: local commands
reject remote flags, and sync, hooks and `remote status` fail without a complete
remote destination. Hooks check that destination before discovery, so an
unconfigured machine never enrolls identities or collects checkouts. Flat
pre-namespace config files are rejected with each key's replacement and a nested
JSON example. `local.host`, `DIFFX_LOCAL_HOST` and `--host` are rejected with
their fix, because the viewer binds loopback only; personal network access
requires explicit `diffx remote` TLS serving or SSH forwarding. Misplaced keys name their target section and field; invalid values
include accepted examples. Diagnostics never echo tokens or credential-bearing
URLs, and rejected files remain unchanged.
