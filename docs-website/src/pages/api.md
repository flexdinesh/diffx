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
use `/api/v2/contexts/{id}/...`; `/api/v2/events` sends `ingestion` hints and
`comments` hints (`contextId`, `streamId`) after any comment, reply or
resolution change in a review stream.
A context with captured branch commits carries `commits` (`count`, `omitted`).
`GET /api/v2/contexts/{id}/commits` lists them oldest first with change totals;
`.../commits/{oid}/diff`, `.../commits/{oid}/files/{fileId}/patch` and
`.../contents` read one commit's diff against its first parent (mode and
`comparisonKind` `commit`). Commits are read-only; comments and reviewed marks
belong to comparison scopes. Ingestion requests may include up to 100 `commits`
with `commitsTotal`; install the server before collectors that send them.
Latest means the most recently collected submission for each source, repository,
checkout, branch and comparison policy; arrival order breaks ties. It does not guarantee current
filesystem state. Older snapshots stay stale when newer reviews expire.

Comments carry an `author` (`kind` `user` or `agent`, with an agent's reported
`name`) and one level of `replies`. `POST .../comments/{commentId}/replies` adds
a reply as the user; `PATCH` and `DELETE .../replies/{replyId}` edit the user's
own replies and delete any reply. `POST .../review/comments/{commentId}/resolve`
takes an optional `{"note"}` that joins the thread in the same transaction and
records `resolvedBy` and `resolvedAt`. Comment lists accept `author=user` or
`author=agent`.

Compatible coding agents find, answer and resolve review feedback through
`/mcp`; see [MCP](mcp.md). A compatible browser can expose the same tools through
WebMCP while the page is open; see [WebMCP](webmcp.md).

The server keeps its data in PostgreSQL and migrates its schema at startup; see
[server setup](agent-hooks.md). It never resets data automatically: to start
fresh, stop the server, back up the database, and point it at a new one.
CLI sync and hook state lives under `~/.local/state/diffx`; an isolated
`DIFFX_RUNTIME_DIR` moves it and config into that directory.
Legacy commands and pending producer payloads are retired.
Stop older collectors before upgrading.
