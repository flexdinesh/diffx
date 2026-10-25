---
title: API and data
description: REST contract, ingestion protocol, storage and upgrades.
---

# API and data

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
[MCP](mcp.md). A compatible browser can expose the same tools through
WebMCP while the page is open; see [WebMCP](webmcp.md).

Local data lives in `$XDG_STATE_HOME/diffx/state.db`, or
`~/.local/state/diffx/state.db`. An isolated `DIFFX_RUNTIME_DIR` moves the
default database, manual sync and hook state, and config into that directory.
`--state memory` disables persistence.
Current schema 12 state persists across restarts; schema 11 state upgrades in place. When local state comes from
an older diffx, `diffx` asks before moving it to a backup beside the database
and starting fresh; scripts pass `--reset-state`, which backs up and resets
current state too. Only the newest backup is kept. Newer or unrecognized
databases are refused unchanged. Servers never reset automatically: to start
fresh, stop the server, back up its state directory, and explicitly choose a new
database path.
Legacy commands and pending producer payloads are retired.
Stop older processes before upgrading. Local service lifecycle uses a private
authenticated loopback control listener; its token does not authenticate the
local public API.
