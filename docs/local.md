# Local development

Local sessions, state, collection and CLI composition. Commands live in
[development.md](development.md).

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
`DIFFX_RUNTIME_DIR` isolates the local socket, lifecycle discovery, default config, SQLite state,
producer identity and pending delivery state. Its database persists after exit;
reuse the directory to retain that session's history, or remove it after stopping
the process when no longer needed. Choosing a port alone does not isolate state.
Explicit `DIFFX_CONFIG_PATH` can select shared config, and its `local.state`,
`DIFFX_LOCAL_STATE` or `--state` can select a shared database despite distinct
runtime directories. Unset those overrides, as above, or give each session a
distinct state path. SQLite still permits only one owner per database.
Socket paths are limited to 103 bytes on macOS and 107 on Linux; macOS
`mktemp -d` paths under `/var/folders` fit, but deeper runtime directories may
not. Tests that start a local instance create short runtime directories with
`os.MkdirTemp("", "dx")` rather than `t.TempDir()`.

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
persistent database and sign up in the app (signup and password signup default
on). Create API tokens through Settings, which works before any upload. New
accounts activate without a restart. Operator commands require stopping the
database owner.
Credentials scope REST, MCP, jobs and events. Local mode automatically supplies
its stable owner and exposes no account UI or management routes. Do not provision
against the running personal service.

## Collection diagnostics

Local collection, manual sync and hooks save failed attempts automatically under
`config.StateDir()/diagnostics` (normally `~/.local/state/diffx/diagnostics`).
`DIFFX_RUNTIME_DIR` isolates these reports with the rest of the private state.
Use `diffx . --debug` or `diffx sync --debug` for stage progress and a successful
report to compare with failures. Errors include a diagnostic ID and export command:

```sh
diffx diagnostics export --latest --output failure.json
diffx diagnostics export REPORT_ID --output failure.json
```

Reports use schema version 1. They preserve stage totals, command counts, the
latest 128 commands and the 20 slowest, including queue waits and deadline kind.
Cancellation and cleanup have separate phase/error codes. Files are private,
bounded to 1 MiB, and rotated to five reports. Existing output files are refused.
Share the exported JSON for diagnosis; it excludes captured content and raw
process/configuration data and pseudonymizes repository identifiers.

Regression tests exercise large synthetic Git workloads and count real Git
invocations. Compare cold runs on the same machine and include the final contents
and consistency checks; a shorter trace that ends in failure is not a completed
benchmark. `BenchmarkCollectBranchWorkload` reports allocations and `git/op`.

## CLI composition and plugins

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
checks compile the adapters; web and CLI builds leave them unchanged; host SDK dependencies are development types only.
Pi and OpenCode's self-contained `dist/index.js` artifacts are committed so a
clone is ready to install without a build. After changing adapter source, run
`mise run plugins:build` and commit the regenerated artifacts. Codex and Claude
load their checked-in hook definitions through the root marketplace manifests.
