---
title: Agent hooks and remote ingestion
description: Run a remote diffx server and collect changes automatically after agent turns.
---

# Agent hooks and remote ingestion

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
[plugin setup](plugins.md) for native installation on all hosts,
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
and runs a bundled PostgreSQL 18 whose named volume survives stopping or
recreating the containers. Set
`DIFFX_SERVER_PORT=7979` to publish another host port. The project is named
`diffx`, so its database volume is shared by every checkout. Open the app and
create an account; signup and password signup are on by default. No default
account/password is generated. Compose explicitly enables insecure
cookies for loopback HTTP development; set `DIFFX_SERVER_INSECURE_COOKIES=false`
and deploy behind HTTPS for remote access.
See [server development](https://github.com/flexdinesh/diffx/blob/main/docs/development.md#server-development) for configuration
and persistence details. Deploy behind HTTPS when exposing the server remotely.

Or build and run the image directly against your own PostgreSQL 18+:

```sh
mise run docker:build
docker run --name diffx-server --read-only -p 127.0.0.1:7980:7980 \
  --env-file /path/to/private-server.env diffx-server:local
```

The env file sets `DIFFX_SERVER_DATABASE_URL`, for example
`postgres://diffx:…@db.example.com:5432/diffx?sslmode=verify-full`; the server
reads it only from the environment and never logs it. Its role needs `CREATE` on
the database, where the server owns the `diffx` schema and migrates it at
startup. Plain HTTP development requires `DIFFX_SERVER_INSECURE_COOKIES=true`;
behind HTTPS, set `DIFFX_SERVER_PUBLIC_URL` so Google redirects and invite and
reset links use it. The
database contains observations, jobs, accounts and credential hashes; the
container keeps no state. The image runs without Git or a repository mount, as
an unprivileged user. SIGTERM drains HTTP and stops the worker; durable
unfinished jobs recover after their leases expire. Back up the database with
your Postgres tooling.

Build/install the remote server with Go:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx-server@edge
DIFFX_SERVER_DATABASE_URL=postgres://diffx:diffx@127.0.0.1:5432/diffx?sslmode=disable \
  DIFFX_SERVER_INSECURE_COOKIES=true diffx-server
```

Open the app and create an account. Open signup and password signup are on by
default; admins turn them off in the console's Settings page, so accounts come
only from invites, or new accounts cannot choose a password. Existing password
accounts keep working. Google sign-in needs a Google OAuth web client: set
`DIFFX_SERVER_GOOGLE_CLIENT_ID`, `DIFFX_SERVER_GOOGLE_CLIENT_SECRET` and
`DIFFX_SERVER_PUBLIC_URL`, and register
`<public URL>/api/v2/auth/google/callback` as its redirect URI. Then open the top-right account settings to create
a named API token. `collector` tokens support sync and plugins; `api` tokens also
support the user's review REST/MCP APIs. Both expire after 90 days by default
(selectable 1–365 days), can be revoked, and show their secret only once. Browser
sessions use secure cookies; API/MCP and collectors use bearer tokens. Multiple
users share one Postgres database with private data per account. Local `diffx`
automatically supplies its owner and has no login or account settings.

Invites, signup settings, password reset links and account administration live
in the admin console, a separate `diffx-console` container on its own origin over the same
database. Admins use the app like any other account. Compose runs it on
`127.0.0.1:7970`; on first start, find the setup link in
`docker compose logs console` and open it within 24 hours to create the first
admin with a password or Google. Admins invite members, turn signup and
password signup on or off in Settings, promote and demote accounts,
disable or delete them, and issue password reset links. Disabling revokes the
account's sessions and tokens, and deleting removes everything it owns. A
locked-out admin gets a reset link with the console's environment:

```sh
diffx-console user reset-password --email alice@example.com
```

Password reset revokes browser sessions; API token
ownership is independent of passwords. Deploy behind TLS with secure cookies.
Use `--retention-days 14` or config/environment to change server retention.
The server defaults to `127.0.0.1:7980`, independent of personal CLI settings,
and requires `DIFFX_SERVER_DATABASE_URL`. Its image defaults to `0.0.0.0:7980`;
`DIFFX_SERVER_*` env vars override image defaults, and explicit flags override
env vars. The CLI never reads `DIFFX_SERVER_*`.

Probe `/healthz` for liveness and `/readyz` for readiness; readiness answers 503
while the database is unreachable. Both are unauthenticated and uncached.
Sign-in rate limiting is off by default. Set `DIFFX_SERVER_AUTH_RATE_LIMIT=true`
to allow each client IP 20 sign-in attempts per minute across every replica. It
requires `DIFFX_SERVER_CLIENT_IP_HEADER`, such as `X-Real-IP`: a header your
trusted ingress overwrites with the single client IP. Without that ingress,
clients could choose their own IP, so leave limiting off.

Collectors select that destination with config `remote.url`, `DIFFX_REMOTE_URL`
or `--server`, and authenticate with config `remote.token`, `DIFFX_REMOTE_TOKEN`
or `--token`:

```sh
diffx sync --server https://reviews.example.com
```

Remote submission does not start a local server. The remote server needs no Git
installation or repository mount: all queries read committed Postgres data.
Other producers can submit the same `POST /api/v2/ingestions` contract directly;
the bundled plugins invoke the Go collector.
