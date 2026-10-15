# diffx

Review local Git changes in your browser without modifying your working tree or
index.

## Install

With Homebrew:

```sh
brew install flexdinesh/tap/diffx
```

Latest stable release with Go 1.25 or later:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@latest
```

Or install a specific stable version:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@v0.2.0
```

Or install the latest development changes from `main`:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@main
```

## Usage

Capture the selected checkout in one foreground process:

```sh
diffx
diffx .
diffx /path/to/repo --host 0.0.0.0
diffx . --replace
diffx . --base HEAD --no-browser
```

Collection and ingestion run in-process. The same process serves the web UI,
REST and MCP until Ctrl-C. The path defaults to the current directory and resolves
to its containing checkout, including linked worktrees. Only that checkout is
collected. The browser opens the captured snapshot; later edits and branch switches
do not change it until you explicitly refresh, which captures a new snapshot of
the same checkout. Observations and comments persist across restarts in the
shared local database; each process shows only its own checkout or piped diff.
Failed collection never substitutes an empty diff.

Only one local instance runs. Starting another prompts before replacing it;
scripts must pass `--replace`. Replacement requests authenticated graceful
shutdown and waits for database ownership. Unreachable instances are not killed
using an unverified PID.

Publish once to a remote server, including every registered worktree:

```sh
diffx config set remote.url https://reviews.example.com
diffx config set remote.token - < /path/to/private-token
diffx remote status
diffx sync
diffx sync --print
diffx sync --debug
diffx sync --retry
```

Sync collects only the selected checkout and waits for pending observations to
commit. Unchanged uploads are skipped after server freshness checks. `--print`
prints only the configured server URL on success; progress/errors use stderr. `--debug` uses one updating
terminal line, or plain lines when redirected. Failed uploads remain private,
immutable pending submissions; `sync --retry` recovers them without the checkout.
Cancelling a wait does not cancel an accepted server job.
After the destination account has been verified, rotating its API token preserves
pending-upload recovery. Captures made before any successful authentication stay
bound to their original credential until the account is verified.

`diffx .` and piped diffs always run locally, even with remote config. Sync
and plugins require a remote URL and bearer token; they never bootstrap a local
server. `diffx remote status` checks the destination, credential and protocol,
and reports agent-hook deliveries still waiting.

The all scope includes branch changes from the local default branch's merge base
plus working changes. Staged/unstaged retain HEAD/index semantics.
`--base HEAD` selects working changes only. Collection never fetches Git refs.
Matching captures reuse review state. Retention defaults to seven days after the
last fresh submission; retries do not extend it.

Defaults live in `~/.config/diffx/config.json`; `--config-file` or
`DIFFX_CONFIG_PATH` overrides the location. One file configures both modes in
separate sections, so a local review and a remote server can run side by side:

```json
{
  "local": { "host": "127.0.0.1", "port": null, "retentionDays": 7 },
  "remote": { "url": "http://127.0.0.1:7980", "token": "…" }
}
```

`local.*` configures only `diffx [PATH]` and piped diffs; `remote.*` configures
only `sync` and agent hooks. Environment overrides use the same namespaces:
`DIFFX_LOCAL_HOST`, `DIFFX_LOCAL_PORT`, `DIFFX_LOCAL_STATE`, `DIFFX_LOCAL_WEB_DIR`,
`DIFFX_LOCAL_RETENTION_DAYS`, `DIFFX_REMOTE_URL` and `DIFFX_REMOTE_TOKEN`.
Precedence: defaults, JSON, environment, explicit flags. `diffx config
{set|get|remove} KEY` edits persisted settings by dotted key, such as
`local.port` or `remote.url`. Token reads are masked; `set remote.token -` reads
stdin. Older flat config files are rejected with the key's new name; move each
key into its section.

The default local listener binds to `127.0.0.1` on an available port from 7981–7990.
`--port 0` chooses an OS-assigned port. A non-loopback listener exposes local
reviews to reachable clients. The browser opens a loopback URL for wildcard binds.

Use `diffx --help` for usage. Legacy `review`, `serve`, `service` and
`capture` commands are removed. Development fixtures use `diffx dev --fixture`.

## Agent hooks and remote ingestion

Install Codex, Claude Code, OpenCode or Pi plugins to synchronize each
worktree automatically after agent completion:

```sh
codex plugin marketplace add flexdinesh/diffx
codex plugin add diffx@diffx
```

Configure a remote server and token first. Plugins are remote-only.
The plugin returns after scheduling; a detached Go worker collects only the
triggering checkout using the same branch comparison as manual review.
Non-Git directories are ignored. Child repositories and branches without live
checkouts are excluded; use `sync --branch` for explicit object-only recovery.
Persistent Git identities survive checkout moves and branch renames.
Initially clean automatic captures are skipped. Dirty-to-clean transitions and
new session associations are submitted. Unchanged uploads are skipped, overlapping requests
coalesce, and failures stay outside the agent conversation. See
[plugin setup](docs/plugins.md) for native installation on all hosts,
configuration and removal. Plugins are ready to install from the repository;
the diffx executable is installed separately and must be available to the
harness through PATH or `DIFFX_BINARY`.

Inspect collection/delivery activity and retry waiting payloads:

```sh
diffx collector status
diffx collector retry
tail -n 50 ~/.local/state/diffx/hooks-v4/hooks.log
diffx sync --branch feature --base main --no-browser
```

Manually schedule the same detached collection used by harness plugins:

```sh
diffx hook --harness codex --path /path/to/repo
```

To collect synchronously with harness metadata:

```sh
diffx sync --trigger agent-hook --harness codex --run-id run-123 --session-name 'Feature work' --no-browser
```

Use `--source-id` to supply a stable source/container identity. Hostnames,
branches and source-local paths are searchable metadata, not global identities.
Different sources, checkouts, branches, HEADs, or diff contents remain
independently reviewable. Identical content from the same checkout reuses its
review context. Sessions associate with shared reviews; they do not own or
claim authorship of every worktree's edits. Harness, session ID and optional
session name remain searchable even when identical content reuses a review.

Build and run the container setup for server development:

```sh
mise run docker:up
mise run docker:down
```

Compose publishes HTTP on `127.0.0.1:7980`, outside the local 7981–7990 range,
and preserves the named SQLite volume when stopped or recreated. Set
`DIFFX_SERVER_PORT=7979` to publish another host port. The project is named
`diffx`, so its volume is shared by every checkout. Before first start, put
`DIFFX_SERVER_USERNAME=alice` and `DIFFX_SERVER_PASSWORD=…` in a git-ignored
`.env` beside `compose.yaml`. No default account/password is generated. These
bootstrap credentials create the first account once; restarts preserve password
changes. Signup is disabled by default. Compose explicitly enables insecure
cookies for loopback HTTP development; set `DIFFX_SERVER_INSECURE_COOKIES=false`
and deploy behind HTTPS for remote access.
See [server development](docs/development.md#server-development) for configuration
and persistence details. Deploy behind HTTPS when exposing the server remotely.

Or build and run the image directly:

```sh
mise run docker:build
docker run --name diffx-server --read-only -p 127.0.0.1:7980:7980 \
  --env-file /path/to/private-server.env \
  --mount source=diffx-data,target=/data diffx-server:local
```

The env file supplies bootstrap username/password; plain HTTP development also
requires `DIFFX_SERVER_INSECURE_COOKIES=true`. The volume contains observations,
jobs, accounts and credential hashes.
The image runs without Git or a repository mount, as an unprivileged user.
SIGTERM drains HTTP and stops the worker; durable unfinished jobs recover after
their leases expire. Back up the database while stopped.

Build/install the remote server with Go:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx-server@main
DIFFX_SERVER_USERNAME=alice DIFFX_SERVER_PASSWORD=… \
  DIFFX_SERVER_INSECURE_COOKIES=true DIFFX_SERVER_STATE=./test-data/state.db diffx-server
```

Sign in with username/password, then open the top-right account settings to create
a named API token. `collector` tokens support sync and plugins; `api` tokens also
support the user's review REST/MCP APIs. Both expire after 90 days by default
(selectable 1–365 days), can be revoked, and show their secret only once. Browser
sessions use secure cookies; API/MCP and collectors use bearer tokens. Multiple
users share one SQLite database with private data per account. Local `diffx`
automatically supplies its owner and has no login or account settings.

Set `DIFFX_SERVER_SIGNUP_ENABLED=true` to allow signup; invitations are not yet
supported. For operator provisioning or password recovery, stop the server, run:

```sh
diffx-server user create --username bob --state /data/state.db --password-stdin < /path/to/private-password
diffx-server user reset-password --username alice --state /data/state.db --password-stdin < /path/to/private-password
```

Then restart the server. Password reset revokes browser sessions; API token
ownership is independent of passwords. Bootstrap requires both
`DIFFX_SERVER_USERNAME` and `DIFFX_SERVER_PASSWORD`; it never overwrites an existing
account's password. Deploy behind TLS with secure cookies.
Use `--retention-days 14` or config/environment to change server retention.
The server defaults to `127.0.0.1:7980` and `./data/state.db`, independent of
personal CLI settings. Its image defaults to `0.0.0.0:7980` and `/data/state.db`;
`DIFFX_SERVER_*` env vars override image defaults, and explicit flags override
env vars. The CLI never reads `DIFFX_SERVER_*`.

Collectors select that destination with config `remote.url`, `DIFFX_REMOTE_URL`
or `--server`, and authenticate with config `remote.token`, `DIFFX_REMOTE_TOKEN`
or `--token`:

```sh
diffx sync --server https://reviews.example.com
```

Remote submission does not start a local server. The remote server needs no Git
installation or repository mount: all queries read committed SQLite data.
Other producers can submit the same `POST /api/v2/ingestions` contract directly;
the bundled plugins invoke the Go collector.

## Reviewing changes

The Projects drawer lists repositories by recent activity, each with its current
streams: one per source, checkout, branch and comparison. Each stream shows its
newest snapshot only; clean or merged streams and superseded snapshots are hidden.
A dot marks streams with changes you have not opened in this browser. Search
projects to filter current streams by repository, branch, worktree, host or agent.
Collapse the drawer from its header; it stays collapsed across visits. The header
names the open review's repository, branch and worktree. In a local checkout
process, **Refresh** (Alt+R) collects the checkout again and opens the new snapshot.
Collapsed files and folders are remembered across reloads in the same tab. When a newer snapshot
arrives for the stream you are reviewing, a notice offers **View latest** without
replacing the open review.

Browse all reviews (Ctrl/Cmd+K) to reach older or empty snapshots. Select a
repository, then an observation. Search/filter stored metadata to find
a branch, worktree, host, source or agent run. Switch between its collected all,
staged and unstaged scopes. Use the file tree to navigate, mark files as reviewed
and comment on lines. **Copy unresolved** and **Copy all** export comments as
agent-ready XML.

Observations are immutable. New content creates another observation; matching
submissions associate new provenance with an existing review without changing
its captured diff. Comments and reviewed marks belong to the checkout's stream
(or one piped capture) and are stored in SQLite: later snapshots re-anchor
comments to moved code, flag outdated ones, and keep marks on files whose patch
is unchanged. Deleting one snapshot keeps them; they end with the stream's last
snapshot. Display preferences remain in the browser.

## Piped diffs

Review a fixed patch in a foreground local process:

```sh
git diff | diffx
git show | diffx
diffx < saved.patch
```

The process serves the UI, REST and MCP until Ctrl-C, without watching a checkout.
Piped diffs ignore remote configuration. `diffx -` explicitly selects stdin;
combining redirected stdin with a path argument or `--path` is rejected.
Collection does not inspect Git or attach repository identity.
Piped diffs have only the all scope and do not
include full file contents. Standard Git patches are limited to 16 MiB total
and 2 MiB per file. Combined merge diffs are shown against the first parent.
Use `git show --diff-merges=separate` to review every parent separately.

## API and data

The OpenAPI contract is served at `/openapi.yaml`. `POST /api/v2/ingestions`
validates and atomically commits an observation; acknowledgement means commit,
not collection scheduled. Replaying the same source/submission identity and
payload returns its original receipt. Reusing it with different payload fails.
Hooks persist bounded immutable payloads before delivery and retry after later
triggers or `diffx collector retry`. Their pending-data retention remains
seven days independently of server retention. Manual commands always submit and
report failures directly.

Collectors and servers require ingestion protocol 4; upgrade them together.
Public v1 routes and synchronous ingestion are removed.

`/api/v2/contexts` lists stored observations with `q`, `repository`, `branch`,
`worktree`, `hostname`, `sourceId`, `runId`, `harness`, `sessionId` and `sessionName`
filters; `latest=true` omits stale snapshots. Each context names its `streamId` and
the stream's current `latestContextId`. Session filters search submission associations. Scoped review operations
use `/api/v2/contexts/{id}/...`; `/api/v2/events` sends ingestion notifications.
Latest means the most recently collected submission for each source, repository,
checkout, branch and comparison policy; arrival order breaks ties. It does not guarantee current
filesystem state. Older snapshots stay stale when newer reviews expire.

Compatible coding agents can discover stored reviews through `/mcp` or use the
printed `/mcp/contexts/{id}` URL; see
[docs/mcp.md](docs/mcp.md). A compatible browser can expose the same tools through
WebMCP while the page is open; see [docs/webmcp.md](docs/webmcp.md).

Local data lives in `$XDG_STATE_HOME/diffx/state.db`, or
`~/.local/state/diffx/state.db`. `--state memory` disables persistence.
Current schema 11 state persists across restarts. Older, newer or unrecognized
databases are refused without an automatic reset. To start fresh, stop the
server, back up its state directory, and explicitly choose a new database path.
Legacy commands and pending producer payloads are retired.
Stop older processes before upgrading. Local service lifecycle uses a private
authenticated loopback control listener; its token does not authenticate the
local public API.

For development and architecture, see [docs/development.md](docs/development.md)
and [docs/architecture.md](docs/architecture.md). Publishing instructions:
[docs/release.md](docs/release.md).
