# Architecture

Diffx has one stored-data application with two compositions. This is the
canonical record of package boundaries and architectural decisions.
[System semantics](system.md) covers identities and durability;
[development](development.md) covers commands and checks.

## Modes are compositions

| Concern    | Local `diffx [PATH]`                                                | Remote `diffx-server`                    |
| ---------- | ------------------------------------------------------------------- | ---------------------------------------- |
| Collection | Selected checkout(s) at start, live page loads and sync; stdin once | Separate finite sync or hook producer    |
| Admission  | Direct application call; startup runs after serving                 | Authenticated durable queue and worker   |
| Lifetime   | Foreground; authenticated singleton replacement                     | Foreground; graceful worker shutdown     |
| Storage    | One owner of persistent SQLite                                      | One owner of persistent SQLite           |
| Queries    | Selected checkout(s) or capture via REST/MCP                        | Same application services via REST/MCP   |
| Access     | Public listener; private lifecycle credential                       | Account credentials wrap the application |
| Identity   | Automatic stable local owner; no account UI                         | Account sessions and scoped API tokens   |

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

Private local control supports authenticated status/shutdown only. Replacement
uses lifecycle locking, a validated descriptor and graceful shutdown; a PID
alone is never authority. `diffx dev --fixture FILE` is isolated from
personal lifecycle discovery.

Independent local sessions use distinct `DIFFX_RUNTIME_DIR` directories and
listeners (`--port 0` selects a free port). The runtime directory owns lifecycle
discovery and default config/state paths; explicit config or state overrides can
still point sessions at shared state. Each SQLite database retains one process
owner. See [development.md](development.md) for isolated checkout commands.

## Modules own behavior

- `review`: data, source contract and pure comment/patch rules.
- `collector`, `diffsource`: Git, discovery, parsing and finite collection.
- `ingestion`: versioned request/job contracts, validation and HTTP client.
- `contextservice`: owner-scoped publication, resolution and catalog.
- `reviewservice`: shared snapshot reads, comments, marks and capability checks.
- `reviewdata`: storage-independent identities, results and errors.
- `reviewsession`: a resolved review and its source/policy capabilities.
- `identityservice`: account, admin setup, password, session, API-token,
  account administration, invite and Google sign-in policies.
- `google`: Google OpenID Connect wire adapter: authorization URL with PKCE and
  nonce, code exchange and ID token claim checks. No storage or diffx policy.
- `github`: GitHub wire adapter: app JWT, installation tokens, OAuth web flow,
  REST reads, webhook signatures and payloads. No storage or diffx policy.
- `githubsync`: GitHub integration rules: which accounts may follow which
  repositories, sealed user tokens, webhook deduplication, coalesced pull
  request refresh intent, reconciliation, fetch leases and server-authored
  pull request observations. Remote composition only.
- `fetchprotocol`: lease/result contract and client between `diffx-server`'s
  internal listener and `diffx-fetcher`.
- `fetcher`: `diffx-fetcher`'s partial-clone mirrors and offline collection of
  leased pull request heads. It holds no account, GitHub App key or store.
- `reviewstore`: SQLite transactions, identity persistence, stored reads,
  retention and queue adapter.
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
- `serverapp`: transport/task composition; `localserver`: local lifecycle;
  `controlapi`: its private authenticated control API; `remoteserver`: remote
  authentication and worker composition.
- `config`, `processlock`, `browser`, `version`: configuration and private state
  paths (`StateDir`, honouring `DIFFX_RUNTIME_DIR`), process ownership,
  browser launch and build metadata; `processmetrics`: server process metrics;
  `webui`: embedded assets. `testsupport` is test-only.

Interfaces live beside consumers and require the needed operation. Optional
interface assertions must not change application behavior. Storage contracts
describe atomic application operations, not independently committed CRUD steps.

Local composition resolves its stable owner automatically and exposes no account
management routes. Remote composition resolves sessions or API tokens to opaque
account IDs before application access. Review operations receive an owner without
mode branches. `remoteserver` activates owner-scoped services on demand, including
for accounts created after startup; requests, events and workers share that owner
registry and its shutdown lifetime; disabling or deleting an account stops its
services. Identity service owns credential and administration rules, including
the single admin; transports own cookies, CSRF and route permissions, such as
admin routes requiring the admin's browser session.

The frontend identity owner loads the explicit server mode before mounting review
state. Distributed login and settings work without any observations. Account
changes dispose requests, event streams and workspace state. Local composition
opens reviews directly without exposing identity controls.

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

`mise run test:contracts` runs production dependency checks, frontend token
guards, shared Go/TypeScript comment examples, the Go suite (including
storage/lifecycle/queue/transport boundary tests) and real HTTP schema
validation. Local `mise run check` and pre-push run these checks alongside the
web unit/DOM/browser, plugin and docs-site suites. Full checks and pre-push keep
critical suites mandatory and serialize across linked worktrees using the Git
common directory and `processlock`. Heavy phases run sequentially, Chromium uses
one worker, and Go parallelism is bounded. The runner owns child cancellation and
cleanup before releasing the lock; failed phases fail the invocation. Build output
is reused within the invocation. Standalone suite commands remain available and
are outside that full-check lock.
`test:contracts:rules`
holds only the non-Go guards, because `check` and pre-push already run every Go
package; no hand-maintained package list can miss a new boundary test. CI and
release verification retain static checks, builds, generated-file consistency
and the distribution API smoke test; full suites and guards stay local.

`mise run check:boundaries` classifies every package under `cmd/` and `internal/`
using `tools/check-boundaries`. Unclassified packages, forbidden dependencies and
production imports of `testsupport` fail. Process execution and SQL access stay
with their explicit owners. The checker inspects production imports for the
current build platform; tests may import adapters to exercise their contracts.

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
those remain review responsibilities. `main` requires the GitHub Actions `checks`
status through the `ci-required` ruleset. That status covers lightweight CI;
architecture/design guards and full-suite validation are enforced locally by
pre-push and recorded in the PR evidence.

## Adapter and schema evolution

New adapters must preserve ownership, atomic publication, retention, replay,
lease fencing and commit-before-ack recovery. External brokers require atomic
scheduling intent (an outbox). Multiple server processes need a storage backend
designed for that concurrency; SQLite retains one owning process.

Only public v2 routes and ingestion protocol 4 remain. Retired: `review`,
`service`, `serve`, `capture`, background daemon startup, public v1,
synchronous ingestion and legacy capture/worktree storage.

Diffx starts with fresh configuration, state and producer identities under its
own directories. There are no aliases or migration paths. Ingestion protocol 4
uses `X-Diffx-State` to pin submission and polling to the destination identity;
older protocols are rejected. Release artifacts use the explicit `diffx` project
name, independent of the checkout directory.

Schema 14 preserves current state across starts. It adds captured branch commit
tables (`observation_commits`, `observation_commit_ranges`); schema 13 added
administration and Google sign-in tables and schema 12 GitHub integration tables. Schema 11 moved review state
from each snapshot scope to its stream and scope, because comments and reviewed
marks must survive new collections and snapshot deletion. Schema 11 and later
upgrade in place: later schemas only add tables and indexes, so opening runs the
idempotent creation statements and records the new version in one transaction,
because durable server volumes must gain GitHub integration, administration and
commit state without an operator reset. A schema change that alters existing tables needs a new
compatibility floor instead. Older-than-11, future and unrecognized schemas are
refused; opening a database never resets it. `reviewstore.Reset` is the only reset: under the
ownership lock it moves current or older diffx state and its SQLite sidecars to
a timestamped backup beside the database, keeping only the newest backup, and
refuses newer or unrecognized files. Only local composition decides to call it,
because local state is one developer's re-collectable workspace: `diffx` asks on
a terminal for older state, and `--reset-state` (also on `diffx dev`) consents
up front for any wipe-safe state. Durable server volumes require an operator
decision, so `diffx-server` never resets: stop the owner, back up its state
directory, then select a fresh database path. State errors identify the
database and backup directory, name the compatible schema, and show the
`--state` override for explicitly selecting fresh state; local refusals of older
state also suggest `--reset-state`. Fresh state has no saved reviews or server
accounts.
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
personal CLI config. The binary defaults to loopback port 7980 and a separate
`./data/state.db`; the container supplies overridable env defaults for its
listener and `/data` volume. One process owns the volume's SQLite database,
credentials and durable queue.

Personal CLI configuration keeps modes in separate namespaces so one machine runs
both compositions at once: `local.*` and `DIFFX_LOCAL_*` configure only
`diffx [PATH]` (ports 7981–7990), `remote.*` and `DIFFX_REMOTE_*` only sync and
hooks. Mode is chosen by the command, never by a config field: local commands
reject remote flags, and sync, hooks and `remote status` fail without a complete
remote destination. Hooks check that destination before discovery, so an
unconfigured machine never enrolls identities or collects checkouts. Flat
pre-namespace config files are rejected with each key's replacement and a nested
JSON example. Misplaced keys name their target section and field; invalid values
include accepted examples. Diagnostics never echo tokens or credential-bearing
URLs, and rejected files remain unchanged.
