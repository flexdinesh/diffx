---
title: Usage
description: Capture checkouts, sync to a remote server, and configure diffx.
---

# Usage

## Debugging collection failures

Failed collections save a private diagnostic report automatically. The error
includes its ID and an export command. Export a portable JSON file to attach to
a debugging conversation:

```sh
diffx diagnostics export --latest --output failure.json
diffx diagnostics export REPORT_ID --output failure.json
```

Reports identify collection stages, workload, command counts, timings and the
deadline or error that stopped work. Cancellation/cleanup errors are reported
separately. They exclude source contents, patches, credentials and raw process
configuration; repository paths, refs and URLs are pseudonymized. The five newest
reports are kept, each at most 1 MiB, under the private state directory. Export
requires a new output file. `DIFFX_RUNTIME_DIR` also isolates diagnostics.

Run `diffx . --debug` or `diffx sync --debug` to show collection stages and save
successful reports for comparison. Hooks save collection failures automatically
and reference the diagnostic ID in their activity log.

## Local collection

Open the selected checkouts in desktop by default:

```sh
diffx
diffx .
diffx . --scan-worktrees
diffx . --browser
diffx . --no-browser
diffx /path/to/repo --headless
diffx ~/work --scan-depth 4
diffx --path ~/work --path ~/oss --scan-depth 4
diffx . --replace
diffx . --base HEAD --no-browser
```

The CLI returns after desktop accepts the launch. Desktop owns a socket-only
personal server; explicit `--port` adds a browser viewer. `--browser` instead
opens a foreground browser viewer, `--no-browser` serves that viewer without
opening a client, and `--headless` serves only the socket in the foreground.
If desktop cannot accept startup, diffx falls back to foreground browser serving.
Later server failures remain visible in desktop. Collection and ingestion run
in the personal server process. The path defaults to the current directory. When
that path, or one positional path, is inside a repository, diffx collects only
that checkout; add `--scan-worktrees` to also collect the repository's other
worktrees (`--scan-depth` does not apply there). With `--path` (repeat it for
several) or a directory outside a repository, a path inside a repository
resolves to its checkout; otherwise diffx searches up to `--scan-depth` levels
(default 8) below it, stopping at each repository and skipping hidden,
`node_modules` and `vendor` directories. Every registered worktree of each
repository found is collected, including worktrees outside the paths. Once collection finishes, the client
opens the changed checkout containing the path, otherwise the most recently
active changed snapshot; later edits and branch switches do not change it until you sync, which
collects the same scope again and captures a new snapshot of every checkout in it. A
checkout that fails to collect is reported without blocking the others and never
substitutes an empty diff. Observations and comments persist across restarts in
the shared local database; each process shows only its discovered checkouts or
piped diff.

Only one local instance owns the database. Desktop uses one window: the same
target focuses it, and changing workspace or connection requires confirmation.
Cancel preserves the current session. A compatible scope can attach without
transferring ownership. Foreground scope replacement prompts; scripts must pass
`--replace`. Replacement uses owner-only socket control and waits for database
ownership. Unreachable instances are not killed using an unverified PID.
Quitting desktop stops only an owned local server; attached and remote servers
keep running.

Publish the selected checkout once to a distributed server:

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

Choose **Uncommitted** to review your edits before committing, **Branch changes**
to review commits against the captured merge base, or **Combined** to review both
together. Within Uncommitted, choose **All**, **Staged**, or **Unstaged**. These
views use one captured snapshot; switching never stages files or reads newer Git
state. Comments and reviewed marks belong to their comparison.

Without a saved selection or explicit URL scope, captured staged or unstaged
edits open Uncommitted; otherwise Branch changes opens when available. Combined
uses the default branch's nearest merge base (local or remote-tracking), plus
working changes. `--base HEAD`, or an unavailable branch baseline, provides only
Uncommitted with its staging views. Older snapshots show the comparisons they
actually captured. Collection never fetches Git refs.

A branch snapshot also captures the branch's commits since the merge base (the
100 newest). Choose **Commits** to step through them oldest to newest with the
older/newer buttons, the filterable commit list, or Alt+[ and Alt+]. Each commit shows its
message and its own diff against its first parent; merge commits have no diff.
Commit views are read-only: comment and mark files in **Branch changes**, which
**Open in Branch changes** on each file header opens. `?commit=<oid>` links to a
commit.

Matching captures reuse review state. Retention defaults to seven days after the
last fresh submission; retries do not extend it.

Defaults live in `~/.config/diffx/config.json`; `--config-file` or
`DIFFX_CONFIG_PATH` overrides the location. One file configures both modes in
separate sections for personal serving and the distributed sync destination:

```json
{
  "local": { "port": null, "retentionDays": 7 },
  "remote": { "url": "http://127.0.0.1:7980", "token": "…" }
}
```

`local.*` configures personal checkout and piped-diff serving; `remote.*` configures
only `sync` and agent hooks. Environment overrides use the same namespaces:
`DIFFX_LOCAL_PORT`, `DIFFX_LOCAL_STATE`, `DIFFX_LOCAL_WEB_DIR`,
`DIFFX_LOCAL_RETENTION_DAYS`, `DIFFX_REMOTE_URL` and `DIFFX_REMOTE_TOKEN`.
Precedence: defaults, JSON, environment, explicit flags. `diffx config
{set|get|remove} KEY` edits persisted settings by dotted key, such as
`local.port` or `remote.url`. Token reads are masked; `set remote.token -` reads
stdin. Older flat config files are rejected with the key's new name; move each
key into its section. `local.host` and `DIFFX_LOCAL_HOST` were removed: delete
`"host"` from the `local` section of an existing config file, and unset the
variable.

Personal REST, MCP and web assets are served on an owner-only Unix socket,
`diffx.sock` in the private runtime directory. Default desktop sessions use that
socket directly. Foreground browser serving uses `127.0.0.1` on an available
port from 7981–7990, or `--port` (`--port 0` chooses an OS-assigned port).
`--headless` serves only the socket. Viewer and desktop/browser gateway origins
remain loopback; `--host` was removed.

## Personal TLS connections

On the machine containing your checkouts, run a foreground personal server
(use tmux when needed):

```sh
diffx remote ~/work
diffx remote ~/work --listen :8443 --advertise devbox:8443
```

The default TLS listener is `:7981`, binding network interfaces. `--listen`
chooses a bind address and `--advertise HOST:PORT` sets the address in invitations.
Without explicit advertisement, a concrete bind address is used; wildcard binds
prefer the SSH session server IP, otherwise print non-loopback candidate
addresses. No external address discovery runs. The command uses the same
personal SQLite database and local singleton; it never silently exposes an
already-running local process. Use `--replace` for explicit replacement.
`diffx remote -- status` serves a directory named `status`.

Copy one printed `diffx connect diffx1_…` command to your client machine:

```sh
diffx connect STRING
diffx connect STRING --browser
diffx connect STRING --no-browser
diffx connect STRING --json
```

`STRING` is the full encoded `diffx1_` invitation. It pins the per-run TLS
identity, expires after ten minutes and can be consumed once. Alternative
addresses printed together share that one use. The client saves a run-scoped
credential in owner-only native state, so the original command reconnects
without pairing again during that server run. Restarting the server requires
a fresh invitation. To pair another client or replace an expired invitation,
run `diffx remote pair` on the server machine; optional `--advertise HOST:PORT`
overrides the invitation address. Rotation preserves already-paired credentials.
Pairing state is separate from distributed `remote.url` and `remote.token`;
`diffx remote status` still checks the distributed sync destination.

`connect` defaults to desktop and returns after acceptance. Browser, no-browser
and JSON variants stay foreground until Ctrl-C, serving a filtered loopback
gateway on the first free port from 7991–8000, or `--port`. JSON prints one ready
line with `url`, `target` and `transport` (`tls` for invitations). Native code
holds credentials; they never enter React or browser URLs. The TLS listener
allows pairing and public review/collection routes, excluding private controls,
MCP, accounts and ingestion. Desktop and browser gateways expose the same public
review allowlist. Desktop reconnects transient remote failures with backoff capped
at 15 seconds for two minutes, then offers Retry; mutations are never replayed
automatically. Disconnecting leaves the remote server running.

## SSH connections

Start a personal server on the remote machine, then connect:

```sh
ssh devbox 'diffx ~/work --headless'  # keep running, for example in tmux
diffx connect ssh://devbox
diffx connect ssh://devbox --browser
diffx connect ssh://dee@devbox:2222 --port 7995 --no-browser
```

`connect` runs your system `ssh`, so `~/.ssh/config` aliases, agents and jump
hosts apply. It reads remote `diffx status --json`, forwards the socket to a
native loopback endpoint and opens desktop by default. Browser and JSON
variants stay foreground through the filtered gateway described above. When the
SSH server refuses socket forwarding (`AllowStreamLocalForwarding no`), it
forwards the remote viewer port instead; start that server with `--no-browser`
to provide a viewer. If non-interactive SSH cannot find diffx, pass
`--remote-diffx /full/path/to/diffx`. `--json` retains its `url`, `target` and
`transport` fields (`unix` or `tcp` for SSH), with `url` naming the filtered
gateway. Loopback ports are not per-user; processes able to reach the gateway
can use its review routes. A manual raw socket forward also carries private
control routes, including shutdown:

```sh
ssh devbox diffx status --json   # socket and optional viewerUrl
ssh -N -L 127.0.0.1:7991:/run/user/1000/diffx/diffx.sock devbox
```

`diffx status` reports the running instance's socket, viewer and observation
count, and exits 3 when none is running; `--json` prints one JSON object.
`diffx mcp` serves the running instance's MCP tools over stdio for agents; see
[MCP](mcp.md).

Use `diffx --help` for usage. Legacy `review`, `serve`, `service` and
`capture` commands are removed. Development fixtures use `diffx dev --fixture`.
