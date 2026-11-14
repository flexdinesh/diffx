---
title: Usage
description: Sync checkouts to a diffx server and configure the CLI.
---

# Usage

## Sync

Publish the selected checkout once to a diffx server:

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

Sync and plugins require a remote URL and bearer token. `diffx remote status`
checks the destination, credential and protocol, and reports agent-hook
deliveries still waiting.

## Comparisons

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
older/newer buttons, the filterable commit list, or Alt+Shift+[ and Alt+Shift+]. Each commit shows its
message and its own diff against its first parent; merge commits have no diff.
Commit views are read-only: comment and mark files in **Branch changes**, which
**Open in Branch changes** on each file header opens. `?commit=<oid>` links to a
commit.

Matching captures reuse review state. Retention defaults to seven days after the
last fresh submission; retries do not extend it.

## Configuration

Defaults live in `~/.config/diffx/config.json`; `--config-file` or
`DIFFX_CONFIG_PATH` overrides the location. The `remote` section names the
server that `sync` and agent hooks send to:

```json
{
  "remote": { "url": "https://diffx.app", "token": "…" }
}
```

Environment overrides are `DIFFX_REMOTE_URL` and `DIFFX_REMOTE_TOKEN`.
Precedence: defaults, JSON, environment, explicit flags. `diffx config
{set|get|remove} KEY` edits persisted settings by dotted key, such as
`remote.url`. Token reads are masked; `set remote.token -` reads stdin. Older
flat config files are rejected with the key's new name; move each key into its
section.

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

Run `diffx sync --debug` to show collection stages and save
successful reports for comparison. Hooks save collection failures automatically
and reference the diagnostic ID in their activity log.

Use `diffx --help` for usage.
