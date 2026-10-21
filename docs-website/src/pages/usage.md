---
title: Usage
description: Capture checkouts, sync to a remote server, and configure diffx.
---

# Usage

Capture the checkouts under the selected paths in one foreground process:

```sh
diffx
diffx .
diffx /path/to/repo --host 0.0.0.0
diffx --path ~/work --path ~/oss --scan-depth 4
diffx . --replace
diffx . --base HEAD --no-browser
```

Collection and ingestion run in-process. The same process serves the web UI,
REST and MCP until Ctrl-C. The path defaults to the current directory; repeat
`--path` for several. A path inside a repository resolves to its checkout;
otherwise diffx searches up to `--scan-depth` levels (default 8) below it,
stopping at each repository and skipping hidden, `node_modules` and `vendor`
directories. Every registered worktree of each repository found is collected,
including worktrees outside the paths. The browser opens the newest changed
snapshot; later edits and branch switches do not change it until you sync, which
discovers the paths again and captures a new snapshot of every checkout. A
checkout that fails to collect is reported without blocking the others and never
substitutes an empty diff. Observations and comments persist across restarts in
the shared local database; each process shows only its discovered checkouts or
piped diff.

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

The all scope includes branch changes from the default branch's nearest merge base
(local or remote-tracking) plus working changes. Staged/unstaged retain HEAD/index semantics.
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
