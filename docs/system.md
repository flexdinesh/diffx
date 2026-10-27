# System design

diffx collects Git changes at the producer and pushes complete observations
to a server. The server validates and stores those observations, then serves
them through REST, MCP and a web dashboard. Queries read stored state; they never
ask a producer to inspect a checkout.

This document records the implemented design and its decisions. See
[architecture.md](architecture.md) for package boundaries,
[the user docs](../docs-website/src/pages/usage.md) for commands and [the OpenAPI contract](../packages/api/openapi.yaml)
for transport details.

## Terminology and ownership

| Term                | Meaning and responsibility                                                                                                              |
| ------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Producer            | Code with access to a checkout or supplied patch. Collects data, attaches provenance and submits it.                                    |
| Collector           | The shared Git-aware collection pipeline used by manual CLI commands and agent hooks.                                                   |
| Plugin / agent hook | An event-triggered producer. Can invoke the CLI or implement the ingestion contract directly.                                           |
| Server              | Foreground process owning application processing, SQLite, REST, MCP and web assets. Remote composition also owns admission and workers. |
| Observation         | An immutable snapshot: original metadata, diff scopes, patches and available file contents.                                             |
| Submission          | One collection's provenance and retry identity; may associate with an existing matching observation.                                    |
| Agent session       | Harness, session ID and optional mutable name associated with submissions; does not establish authorship.                               |
| Context             | The server-assigned address for reviewing an observation, including its review state.                                                   |
| Source              | A producer installation or explicitly identified container. Distinct from a repository, branch or hostname.                             |
| Checkout            | A source-specific Git working directory or linked worktree. Owned by the producer.                                                      |
| Account / owner     | The server-established identity that owns stored observations and review state.                                                         |

The CLI owns argument parsing, collection, transport and local lifecycle
bootstrap. The server owns the catalog and its interpretation. The CLI does not
register paths for later server collection or maintain its own catalog.

## Processes and deployment

The application core has two compositions. See [architecture.md](architecture.md)
for contracts, durability rules and migration details.

`diffx [PATH]` is one foreground process: serve UI/REST/MCP immediately, then
collect its selected checkout, or discover and collect the checkouts under its
paths, and ingest directly in a background task. Discovery feeds four workers as checkouts are found; a serial
publisher ingests each completed checkout immediately, so its snapshot becomes queryable
while slower checkouts continue. Publication commits and emits the existing
ingestion event for each observation. The UI opens available snapshots and shows
loading feedback only for missing data; failures remain visible with Retry.
Omitted paths mean the current directory. A bare or positional path inside a
checkout selects only that checkout; `--scan-worktrees` adds its repository's
other registered worktrees, and `--scan-depth` is rejected because a checkout
has no directory search. `--path` (repeatable) and a positional directory
outside a repository discover checkouts and all their worktrees, with
`--scan-depth` (default 8) bounding the search. The scope is decided at startup
and every later sync reuses it. Every path must be an existing directory; missing paths fail before serving. Captured snapshots remain fixed after edits and branch switches until the
user requests collection by loading a live URL or pressing Sync, which discovers
the paths again and ingests a new snapshot of every checkout in scope; nothing watches Git.
Piped input is validated and collected once before serving.
The home-directory database survives process termination, but each process
serves only the checkouts it discovered (every branch and earlier run of them)
or its own piped capture, so other checkouts' history reappears only when reopened.
Only one local process owns the database; another invocation prompts before
authenticated graceful replacement. Noninteractive callers use `--replace`.
There is no collector daemon.

`diffx sync` collects the selected checkout once,
saves immutable pending payloads, and submits them over authenticated HTTP.
The remote server binary owns durable queue admission, the ingestion worker, shared
application services and persistent storage. Sync waits for committed results.
Plugins schedule finite remote-only collection of the triggering checkout with
harness metadata. `diffx remote status` verifies the destination and credential
and reports hook deliveries still waiting, since hooks never write to the agent
conversation.

`diffx-server` runs directly or as the same binary inside a container. It resolves
deployment config without reading or writing personal CLI settings. Container
defaults come from env vars, with SQLite and credential hashes under `/data`.
Compose mounts a named volume and publishes HTTP on host loopback; stopping or
recreating the container preserves data. Only one process may own that database.

Both modes use identical collection, validation, identity, ingestion, catalog
and review operations. Their lifecycle and submission delivery differ.

## Request and response flows

### Collect and ingest

Collection resolves the baseline and all supported comparison scopes together with
full-content identity and bounded immutable previews. It never fetches refs.
Sync and hook collection select one checkout, resolving nested paths to its
root; their other registered worktrees are not collected. A local process
started on one checkout path collects that checkout the same way unless
`--scan-worktrees` is given. Otherwise local collection
resolves each path inside a repository to its root, otherwise walks up to the
scan depth, stopping at repositories and skipping hidden, `node_modules` and
`vendor` directories, then collects every registered worktree of each repository
found, at most 256 checkouts from 4096 directories. Each checkout gets its own
submission ID. Explicit `--branch` recovery remains available for remote collection.

A checkout with a resolved branch baseline captures five scopes atomically:

| Scope         | Captured endpoints         | UI label               |
| ------------- | -------------------------- | ---------------------- |
| `all`         | Merge base to working tree | Combined               |
| `committed`   | Merge base to HEAD         | Branch changes         |
| `uncommitted` | HEAD to working tree       | Uncommitted / All      |
| `staged`      | HEAD to index              | Uncommitted / Staged   |
| `unstaged`    | Index to working tree      | Uncommitted / Unstaged |

HEAD-only collection retains `all`, `staged`, and `unstaged`; its `all` means
Uncommitted. Object-only branch recovery and piped patches retain only `all`,
meaning Branch changes and supplied patch respectively. Every new manifest
declares `comparisonKind`; old manifests receive a read-time interpretation
without changing stored data. Unsupported scopes are unavailable, not empty.

Collection with a resolved branch baseline (checkouts, object-only branch
collection and GitHub pull requests) also captures the branch's
`merge-base..HEAD` commits in the same observation and request. At most the 100
newest (`review.MaxCommits`) are kept, oldest first, ending at the captured
HEAD; `commitsTotal` counts the whole range and the rest are reported omitted.
Each commit records its OID, parents, `git patch-id --stable` (absent for merges
and oversized patches), subject, plain-text body (bounded to 64 KiB), author name
without email, and authored and committed times. A non-merge commit carries its
own diff against its first parent (a root commit against the empty tree), stored
with mode `commit` and previews like a scope; merge commits have no diff.
HEAD-only collection, piped patches and a branch without commits capture none.
`commit` is not a review scope: comments and reviewed marks never target it,
and catalog change counts and scope summaries exclude commit diffs.

Previews are bounded per file and per scope or commit first: a patch over 1 MiB,
or full contents over 1 MiB on either side, are omitted with an explicit
message, and files after the first 1000 of a diff are listed without previews.
Because an observation is one ingestion request and one transaction, a total
limit keeps its encoded JSON within the 64 MiB request limit with 1 MiB of
headroom. Collection captures comparison scope patches, then commit patches,
then scope full contents, then commit full contents, so commits never cost a
comparison its patches and full-file views degrade before files lose their
patch. Each pass splits the remaining total equally between its diffs in rounds,
in file order, skipping a file that does not fit. No diff can consume another's
share, and a share a diff does not need goes to the others. Full-content
identity remains independent of preview truncation.

Local composition calls `contextservice.Ingest` directly as each checkout finishes
collection. Discovery feeds up to four concurrent Git reads, while one serial
publisher commits their completed requests. A slow checkout does not hold already
collected snapshots; a failed checkout never publishes an empty result. Cancellation or a fatal publication
failure cancels and joins the workers. The collection HTTP response still reports
the complete batch and its checkout failures after every worker finishes.
Remote collectors POST to `/api/v2/ingestion-jobs`, receiving 202 with a durable
job/status URL.
Acceptance records owner, payload hash, submission identity and arrival sequence
atomically. A leased worker calls the same ingestion operation. The observation,
scopes, session associations, retry identity and stream head commit atomically.
Then the worker acknowledges the job.

Clients poll owner-scoped status; succeeded includes the context ID.
Transient ingestion notifications remain hints; clients reload durable catalog
state after reconnecting. Queue workers may execute out of order, but stream
freshness follows collection time and acceptance sequence.

### GitHub pull requests

Live testing against a real GitHub App is pending; see
[github-live-testing.md](github-live-testing.md).

The remote server optionally integrates a GitHub App, enabled only when every
`DIFFX_SERVER_GITHUB_*` setting is present; secrets come from the environment
only. Local composition has no GitHub routes.

Access is the intersection of the GitHub user and the app installation. An
account connects GitHub in Settings through the app's OAuth web flow; the
authorization state is bound to the starting account and consumed once, and the
callback, a cross-site top-level navigation, completes only for that account's
session. User access and refresh tokens are sealed with AES-GCM under
`DIFFX_SERVER_GITHUB_TOKEN_KEY`, bound to the account. An account may follow
only repositories GitHub lists for that user's installations. Listing,
following, Resync and startup reconciliation recheck access; delivery to a
follower rechecks it when its last verification is older than eight hours.
Lost access, uninstalls, removed repositories, revoked authorizations and
rejected refreshes mark follows lost; lost follows receive nothing until access
is verified again. GitHub management routes need a browser session and CSRF
token, like account management, because they handle GitHub credentials.

Webhooks arrive at `POST /api/v2/github/webhooks`, authenticated only by the
`X-Hub-Signature-256` HMAC, verified before parsing. A delivery ID is recorded
only after its effects apply, and recorded IDs are acknowledged without effect,
so GitHub redeliveries are safe; every effect is idempotent. Pull request events
for actively followed repositories coalesce into one refresh intent per pull
request by advancing its generation. GitHub never retries failed deliveries, so
the server reconciles once at startup and on Settings Resync: it requests
refreshes for every open pull request of each followed repository and for
previously open ones missing from GitHub's list, which closed without a
delivery. Resync's run identity makes the resulting submissions fresh, extending
retention of pull requests idle since their last change; startup
reconciliation and webhooks do not. There is no periodic poll.

`diffx-fetcher` runs in its own container and turns refresh intent into
snapshots. It leases one pull request at a time from the server's internal
listener (`DIFFX_SERVER_INTERNAL_PORT`, default 7979, never published),
authenticated by `DIFFX_SERVER_FETCHER_SECRET`; the public listener does not
serve the protocol. Leasing reads the pull request with the app's installation
credentials and hands out a token narrowed to that repository with
`contents:read`; the fetcher never holds the app key, a user token or an
account. A deleted pull request is recorded closed and a repository without
active followers completes without a fetch. The fetcher keeps a partial-clone
mirror per repository, fetches the leased head and base, and collects the
branch comparison offline. A result is accepted only for the current lease
token and only if it captures the leased head and base commits; failures retry
with exponential backoff capped at an hour, and an expired lease is reclaimable.

The server, not the fetcher, authors the observation: the `github` source and
trigger, the repository key shared with HTTPS checkouts of the same remote, a
checkout and branch identity per pull request, the pull request URL as root,
`PR #N` as worktree, the head branch, GitHub's update time as collection and
activity time, and the `pullRequest` metadata. Its submission ID hashes that
payload and any Resync run, so refetching an unchanged pull request replays and
a push, edit or state change is a fresh observation in the same stream. Every
follower with verified access is admitted in one transaction with completing
the lease; a refresh requested during the lease stays pending.

### Browse and review

The comparison selector reads scopes of the open observation. It does not
collect Git state, advance to a newer observation, or mutate the index. Explicit
URL scope wins over the saved per-stream selection. Otherwise, captured staged
or unstaged edits select Uncommitted; a clean checkout selects Branch changes
when collected. Scope history preserves the open snapshot and comment draft.
Comments and reviewed marks remain separate per stream and scope; choosing a
new comparison does not migrate review state. Catalog change counts count
distinct paths across every captured scope, so staged edits cancelled by
unstaged edits do not disappear with an empty Combined diff.

When commits were captured, the selector also offers Commits: a read-only lens
on the same observation that steps oldest to newest with previous/next, a commit
list or Alt+[ and Alt+], shows the commit message, and offers “Open in Branch
changes” on file headers. `?commit=<oid>` selects a commit. Review state stays on
comparison scopes.

Navigation is project-driven. The Reviews column queries
`/api/v2/contexts?latest=true`, which omits stale snapshots, and groups stream
heads by repository. A stream is one owner, source, repository, checkout, branch
and comparison policy; every context carries its opaque `streamId` and the
stream's current `latestContextId`. The column hides clean, unavailable and
unknown-status heads, orders streams by recent code activity and repositories by
their most active visible stream, and marks heads this account has not opened in
this browser.
Equal activity uses stable stream and context identities. Unopened heads carry
accent marks in the column, rail and header combobox rows; there is no global
counter. First visits mark existing heads as seen; opening a head clears its mark. Ingestion events reload
the heads; when the viewed context's stream has a newer head, a notice offers it
without replacing the open review. URLs are live or pinned: `/streams/{streamId}`
opens the stream's latest head when loaded or navigated to, while
`/contexts/{contextId}` pins one snapshot. Review rows, rail marks, header
combobox choices, and “View latest” open
live URLs; the picker opens pinned ones; piped snapshots have no stream and stay
pinned. A live URL resolves once, so later arrivals raise the notice instead of
swapping the open review. With local collection enabled, loading `/` or a live
URL syncs the checkouts in the background, joining startup on the first load
through `POST /api/v2/collections?initial=true`. A live URL opens the requested
stream's first available head immediately. `/` waits for collection, because
heads appear as each checkout finishes, then opens the collection's default:
current changed heads first, preferring the checkout containing the process's
path, so `diffx .` in a worktree opens that worktree, then the most recently
active. The open snapshot stays selected when collection finishes; a newer head
raises the “Newer snapshot” notice with “View latest”. Without an available
head, the UI waits for collection; failures remain visible and offer Retry.
Otherwise `/` opens the most recently active changed head's live URL. Startup
selection and the local default MCP context use the same collection default.
The Reviews column leads the navigation island, beside the open review's file
sidebar, aligned with the diff below the header. A remote server always shows
it. A local checkout process shows it, its rail and the compact drawer toggle
only once the column would list two current streams, then keeps it for the
page session; until then the island holds only the file sidebar, while search,
the header comboboxes and sync remain. Switching streams replaces
everything to its right and keeps the column. It collapses to a rail of
repository marks. A repository (its rail mark or its name in the list) opens
its most active stream; the list's chevron alone collapses or expands it. The
column lists streams grouped by repository or as one recent-first list. List
or rail state, grouping and each pane's width persist per browser. The header
names the open review as repository and branch comboboxes that filter as you
type and switch streams. Compact layouts open the column and file sidebar as
drawers from header toggles; choosing a stream closes the drawer. The header's
“Search reviews” magnifier opens the review picker. A capture process
(`input: "capture"` in the local bootstrap: piped stdin or a fixture) serves
one fixed diff, so the shell shows no column, rail, comboboxes, search or
picker; only checkout and remote processes navigate between reviews. An
explicit local sync, like an arrival,
raises the notice instead of replacing the open review. Scrolling the diff
selects its top file in the tree; a file chosen from the tree, the keyboard or a
comment stays selected until it leaves view. Trailing space lets the last file
scroll to the top.

Activity is a producer estimate, separate from collection freshness and unread
state. Checkout collection takes the maximum of HEAD's committer time, changed
paths' modification times and the index modification time when staged changes
exist. Deleted paths use the nearest surviving directory's modification time.
Collection Git reads disable optional locks and diff's automatic index refresh
so they do not manufacture activity by updating the index timestamp during Sync.
Merge-base checkout comparisons include these local-change signals; objects-only
branch collection uses the captured target commit's committer time and ignores
the checkout. Missing activity falls back to captured collection time. Touches,
index rewrites and producer clocks can affect this heuristic. Activity failures
do not discard an otherwise valid diff; cancellation still stops collection.

The optional submission `activityAt` timestamp is projected as catalog
`lastChangedAt`. Current heads use their winning submission, including a fresh
submission that reuses an existing observation. Historical snapshots retain
their original activity; captured observation metadata never changes. Activity
does not participate in content identity, unchanged-upload suppression or stream
head selection. Unchanged submissions can update ordering without producing an
unread-content indicator. Piped history and the full catalog retain their
existing collection/submission chronology.

Optional `pullRequest` metadata (title, description, author, state, refs and
URL of a hosted pull request) follows the same rule: a current head shows its
winning submission's value, so a title edit or merge that leaves the diff
unchanged reuses the observation and still updates what the head shows, while
historical snapshots keep their captured value. It does not participate in
content identity. Only the server's GitHub integration authors it; public
ingestion rejects it with the reserved `github` source and trigger.

The review picker, opened by Cmd/Ctrl+K or the header's “Search reviews”
action, loads the full catalog only while open. It selects a
repository, then an observation identified by worktree, branch, source, run and time. Distinct
content from the same worktree remains independently selectable; unchanged
reviews reuse one context. Search supports repository, branch, worktree,
hostname, source and run metadata, plus harness/session ID/session name through
submission associations. Filters cover All / Latest / Stale, Host,
Branch and Worktree, with OR within a multi-select filter and AND across
filters. Filtering precedes repository grouping and counts. Latest snapshots
with changes are the default; the right-aligned All checkbox also includes
unavailable, empty and unknown-status entries. Unavailable entries remain
disabled and skipped by keyboard navigation. Selections persist while navigating
the picker. New collections make older snapshots stale within the same owner,
source, repository, checkout, branch and comparison policy; collection time wins over upload time,
with arrival order breaking ties. Fresh deduplicated submissions can make an
existing review latest again without changing its captured contents or metadata.
The durable stream head survives expiry and pruning so older retained snapshots
do not become latest through housekeeping.

Opening a context or switching scope loads its stored manifest, patches and
available contents through scoped REST endpoints; a commit reads them through
`/commits/{oid}/...`. MCP uses the same stored context and review services,
reading commits through `list_commits` and a `commit` argument. Preview reads validate scope/version and file
fingerprint through indexed stored identities, then read only that file's
preview. They do not decode the full manifest in Go. Context resolution and
catalog presentation project the captured source kind from stored metadata;
SQLite still parses manifest JSON for source kind and file counts. No server
preview cache or schema change is required.
Comments and reviewed marks are mutable SQLite
state of the stream and scope, spanning its snapshots, so a review continues
across new collections; the captured diff stays immutable. Piped observations
form a stream of their own. Comment responses carry a server-computed placement
in the viewed snapshot. Line comments re-anchor by preserved code: the block
must occur exactly once on its side, preferring a match whose captured
surrounding lines also match; ambiguity never anchors. A found block is
`anchored` at its original lines or `moved`; otherwise it is `unchanged` when
it occurs once in collected full contents, else `outdated`. File comments follow
their path or a rename. Open outdated or unchanged comments can be carried to
the viewed snapshot as file comments keeping their original code and lines.
Reviewed marks carry to snapshots whose stored file patch is identical.

`GET /api/v2/events` supplies transient server-sent ingestion notifications.
The browser reloads the catalog on events and reconnection. An arrival does not
silently replace the observation being reviewed. The event stream is a hint to
query durable state, not a durable queue or an event replay log.

## Data and identity

The conceptual model is:

```text
user
  repository grouping
    observations from any number of sources and checkouts
      captured metadata
      submissions and observing sessions
      immutable captured scopes, each with explicit comparison semantics
        manifest, patches, available contents
    review stream (source/checkout/branch/comparison, or one piped capture)
      mutable review state per scope, spanning its observations
```

An observation can also be unassociated with a repository, such as a standalone
piped patch. Server-assigned context, diff and version IDs identify stored
records; producer paths do not identify server files.

| Identity or metadata | Current meaning                                                                                                                                                                                 |
| -------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Account              | Local default user or remotely authenticated individual; never accepted as an arbitrary producer claim.                                                                                         |
| Repository key       | Enrolled in Git metadata, initially derived from sanitized remote URL or source/common directory; preserved through moves and remote changes. Different remote URL forms may enroll separately. |
| Source ID            | Stable local producer identity, or explicit `--source-id` for a container/installation. Hostname is a searchable label, not this identity.                                                      |
| Checkout key         | Enrolled per source in Git metadata, distinguishing linked worktrees and independent sources and surviving directory moves.                                                                     |
| Submission ID        | Identifies one collection and survives retries. A new collection gets a new ID.                                                                                                                 |
| Provenance           | Repository name, remote URL, path, worktree name, branch, HEAD, resolved comparison, hostname, harness/session, triggering directory, trigger, collection time and collector version.           |
| Stored time          | Server timestamp used for catalog ordering; separate from the producer's collection timestamp.                                                                                                  |

Accounts have stable opaque IDs independent of username, passwords, browser
sessions and API tokens. Username matching is case-insensitive and usernames are
fixed in this version. Local mode automatically resolves one stable OS-backed
owner; users never manage local identity. A remote server has exactly one admin,
the only account with a password; there is no public signup and no default
credential. While no admin exists, each start stores a hashed one-time setup
secret for 24 hours, replacing the previous one, and logs it as
`<public URL>/setup#t=…`. The fragment keeps the secret out of request logs.
Setup consumes the secret and creates the admin in one transaction, so
concurrent attempts yield one admin. Only the admin signs in with a password, at
`/admin/login`; every other account signs in with Google, and a refused password
login is indistinguishable from a wrong password. Offline `user reset-password`
recovers only the admin.

Accounts other than the admin exist only through invites. The admin invites an
email; the link carries a hashed one-time secret in its fragment, is shown once,
expires after 48 hours and replaces that email's pending invite. Accepting
requires Google sign-in reporting a verified email equal to the invite's, and
uses the invite and creates the account in one transaction. The username derives
from the email. Google accounts are identified by their stable subject, not
email: a changed email keeps the account and updates its recorded email. Google
sign-in uses the authorization code flow with PKCE, state and nonce; a short-lived
HttpOnly cookie binds the flow to the starting browser and each state is consumed
once. A Google account without an account or invite cannot sign up. Without Google
configuration the server runs admin-only: Google routes are absent and invites
cannot be created.

The admin manages accounts and invites at `/admin`, linked from its Settings.
It lists accounts and disables, enables or deletes any account except itself. Disabling revokes the account's browser sessions and API tokens in the
same transaction and refuses new ones; enabling restores sign-in, not those
credentials. Deleting removes the account and everything it owns, including
queued ingestion jobs. Both stop the account's active services, and the
ingestion worker refuses jobs for unavailable accounts.

Passwords use salted Argon2id hashes. Browser login creates a revocable opaque
seven-day server-side session in an HttpOnly, Secure, SameSite=Lax cookie. Unsafe
browser requests require a session CSRF token, including account management.
Password changes revoke existing sessions. Login and setup admission is bounded and
login failures do not disclose whether a username exists. Plain HTTP development
must explicitly opt into insecure cookies; production uses HTTPS.

Users create multiple named API tokens in Settings. Secrets appear only on
creation; SQLite stores their hashes and expiry/revocation metadata. Expiry
defaults to 90 days and is selectable from 1–365 days. Two fixed presets apply:
`collector` permits ingestion, job polling, health and reconciliation catalog/
context metadata reads; `api` permits the owner's review REST/MCP, events and
ingestion. Neither preset permits account, password or token management. Those
operations require a browser session. Revocation and expiry take effect at
authentication; rotating credentials does not change account or replay identity.

Each authenticated user can receive observations
from many containers, including identical branch and path names. Source, session
and checkout metadata preserve their differences. Provenance describes what a
producer reported; it is not proof of repository ownership or trustworthiness.
Review state is shared by every snapshot of the same user/source/repository/
checkout/branch/comparison stream. Sessions associate with those shared contexts;
renaming a session updates its searchable label without changing snapshot
metadata. The triggering session observed collection; it does not own every
worktree's edits.

SQLite is the durable query source, including the diff data itself. Reads need
neither Git nor a repository mount. Available full contents are captured within
a collection budget; unsupported or unavailable previews remain explicit.
Snapshot retention is configurable in whole days, default seven, after the last
fresh submission. Changing configuration preserves existing expiry until a
fresh submission; retries do not extend it. Reads enforce
expiry immediately; local and remote servers prune at startup and hourly,
removing all scopes and previews. Commit diffs share their observation's
expiry: fresh matching submissions extend them, and pruning and deletion remove
them. Deleting or pruning a snapshot keeps its
stream's comments and marks while another snapshot of the stream remains; the
same transaction deletes them with the stream's last snapshot. Compact replay
identities and stream heads survive deletion and expiry, preventing replay
resurrection and regression of freshness. Incompatible schemas are refused without resetting data;
only local `diffx` can back up older state and start fresh, after confirmation or
`--reset-state`. See architecture.md for the fresh-state procedures.

## Freshness and delivery

Composition decisions and trade-offs live in [architecture.md](architecture.md).

“Latest” means the most recently collected submission within a source, repository,
checkout, branch and comparison policy; arrival order breaks ties. Delayed uploads remain stale.
It does not mean verified current checkout state. Producer collection clocks and
submission arrival order can differ. The API does not contact producers to
establish freshness.

### Failure and retry semantics

Replay identity is **account + source ID + submission ID**. The server hashes
the validated request representation. A matching replay returns the original
context; different content under the same identity fails with a conflict.
A separate identity uses account, source, repository, checkout, branch, HEAD,
source kind, scopes, explicit comparison kinds, comparison policy and producer
content hash to reuse unexpired snapshots. Absent comparison kinds preserve the
legacy identity algorithm for queued submissions. Captured commit OIDs extend
this identity and the collector's unchanged-upload fingerprint only when
present, so earlier identities are unchanged and a snapshot without commits is
never reused for one with commits.
The hash covers full content before preview truncation, including binary and
untracked files, modes, renames and staged/unstaged state; mtime and ctime are
excluded. Unsupported full identities fail closed and remain independent.
Producers without a hash dedupe only known empty snapshots. Fresh matching
submissions retain separate retry mappings and provenance, refresh catalog
recency and reset expiry without replacing the original snapshot or review.

The CLI performs a bounded retry of the exact collected request after an
eligible transport failure. It does not recollect the checkout during that
retry. A lost response can mean the transaction committed; replay resolves that
uncertainty without creating a second observation. Validation and conflicting
requests fail explicitly rather than being retried as transport errors.

Manual sync suppresses unchanged uploads only with an acknowledgement less than
24 hours old, a matching complete fingerprint and session metadata, the same
server database identity, and a still-current server context. Matching pending
payloads reuse their original submission ID; stale saved collections require a
fresh submission to advance the stream head. Expired or deleted contexts are
recollected rather than suppressed.

Authenticated health establishes a destination identity bound to both account
and database. Verified pending payloads and acknowledgements use that identity,
so another token for the same account can recover uploads without changing replay
identity. Other accounts and databases remain separate. Offline captures whose
owner has not yet been verified stay bound to their original credential until
successful authentication transfers them to the verified destination; a rotated
credential cannot claim unknown-owner payloads.

Hook workers persist bounded immutable pending payloads and acknowledgements.
They retry after subsequent triggers or `collector retry`; no perpetual upload
process or guarantee that an agent hook will run exists. Scheduling remains
credential-specific. After an environment token changes, a separate recovery
worker can replay an already-bound capture only when all other routing settings
match and authenticated account, database and source exactly match its saved
target. Recovery never inspects or recollects the checkout. Unknown-owner
captures stay on their original route. Authentication failures retain queued
recovery for later retry; a durable receipt prevents duplicate recovery workers
from redelivering completed payloads. A newer original trigger stays queued for
retry under its original routing rather than inheriting recovery credentials.
Local acknowledgements
are scoped by session and reconciled with server state before suppressing
uploads. Pending data expires after seven days independently of server retention.
Manual producers report failures. Direct plugins implementing the
contract must preserve submission identity themselves when retrying. A later
manual collection has a new submission ID and may reuse matching content; it
is not a replay of a previous failed submission.

## Local and remote boundaries

Local and remote deployment share application/storage contracts and the HTTP
runtime. Local-only lifecycle discovery is authenticated, private and limited to
status/shutdown for foreground snapshots. Collection in those sessions never uses HTTP.

Remote credentials establish owner identity before review REST, MCP, ingestion
jobs, job status or events can access data. Browser assets and mode/login bootstrap
remain accessible so unauthenticated users can sign in. Settings remain accessible
without reviews; creating a collector token supplies `remote.url`/`remote.token`
configuration. Plugins never fall back to local routing. Production terminates
TLS at the deployment boundary and mounts a persistent database volume.

## Extension direction

Implement new adapters against the existing consuming-package contracts and
behavioral conformance tests. Preserve atomic commit boundaries, owner isolation,
identity bytes and retry semantics. External queues need durable payload/status
storage plus transactional scheduling intent; multiple processes require a
storage backend designed for that concurrency. SQLite retains one owning process.
