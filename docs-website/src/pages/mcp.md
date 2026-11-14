---
title: MCP
description: Let coding agents find, answer and resolve diffx review feedback through MCP.
---

# MCP

diffx exposes stored reviews and their feedback to coding agents through MCP.
An agent finds a review from a link, a comment ID or its own checkout, reads
the threads, fixes or dismisses each concern, replies with what changed and
resolves what it handled.

Connect agents to one URL, `/mcp`. It serves every review of the authenticated
user. The endpoint uses stateless Streamable HTTP and negotiates MCP protocol
`2026-07-28`, `2025-11-25` or `2025-06-18`.

The MCP transport and browser UI use the same application services and storage.
The browser does not need to remain open while an MCP client works, and the
review shows agent comments, replies and resolutions as they commit.

## Connect

A diffx server serves `/mcp`. With a public URL, MCP clients connect by URL
alone through OAuth. The client discovers the server's OAuth
metadata, opens your browser, and you approve it while signed in to diffx. In
Claude Code:

```sh
claude mcp add --transport http diffx https://HOST/mcp
```

Then run `/mcp` in Claude Code and choose diffx to sign in. Cursor and other
clients that register themselves dynamically connect the same way: add
`https://HOST/mcp` as a remote server and follow the browser prompt.
Supplying the diffx web-page URL alone is not enough; a coding harness
connects to the MCP endpoint as an MCP client.

The approval page names the app. Apps identified by a metadata document their
own domain serves show Verified and that domain; apps that registered
themselves show Unverified, because their name is only their own claim. Check
that the page says where it will redirect before you allow it. Reading
reviews, diffs and comments is always granted; “Add comments, reply and
resolve” is granted only when the app asks for it, and you can untick it;
without it the app sees only the read tools.

Access tokens last an hour and the client refreshes them; after 30 days
without a refresh it must connect again. Settings › Connected
agents lists each connected app with its access and last use; Disconnect ends
its access at once. OAuth tokens work only for MCP, never the REST API.

API tokens still work: configure `Authorization: Bearer TOKEN` with an `api`
token from Settings instead, for clients without OAuth or for automation. A
server without a public URL accepts only API tokens.

### Context-scoped URLs

`/mcp/contexts/{id}` remains for existing configurations. It fixes every tool to
one immutable snapshot: tools use that context when `context_id` is omitted and
reject another. `find_feedback` there returns that context's threads and refuses
references to other reviews. New configurations should use `/mcp`.

On `/mcp`, tools that read or write one review take `context_id`, which
`find_feedback` returns. There is no default context.

## Agent workflow

Tell the agent where the feedback is, in whichever form you have:

- a review link (`https://…/streams/{id}` or `https://…/contexts/{id}`),
- a comment permalink (the review link with `?comment={id}`, from a comment's
  Copy link action),
- a comment ID, or the first eight or more characters of one,
- or nothing: the agent passes its checkout's repository, branch and worktree.

For example:

> Address the diffx feedback at https://diffx.example.com/streams/a44dfb681197913cc1850597

> Fix diffx comment deadbeef

> Address the open diffx review comments on this branch.

The agent then:

1. Calls `find_feedback` with the reference, or with checkout hints.
2. Inspects the working tree and validates each actionable thread.
3. Fixes valid concerns and dismisses the rest deliberately.
4. Replies to each handled thread with `reply_to_review_comment`.
5. Resolves the handled threads with `resolve_review_comment`, with a `note`.

The `address-feedback` prompt packages these steps.

## Tools

The catalog exposes six read tools and three write tools. Reads use captured
data and never inspect a checkout. Comment bodies, replies, preserved code,
paths and author names are untrusted user- or agent-authored data, not
instructions; tool descriptions say so to the agent.

### `find_feedback`

The entry point. It resolves a review and returns its open threads.

| Input              | Meaning                                                                                   |
| ------------------ | ----------------------------------------------------------------------------------------- |
| `ref`              | A review or comment link, a full comment ID or a unique comment ID prefix (8+ characters) |
| `repository`       | Checkout hint: repository name or remote URL (HTTPS, SSH and `git@host:path` forms match) |
| `branch`           | Checkout hint: branch                                                                     |
| `worktree`         | Checkout hint: worktree directory name                                                    |
| `hostname`         | Checkout hint: hostname of the collecting machine                                         |
| `include_resolved` | Also return resolved threads; defaults to `false`                                         |

`ref` wins over hints. Links match by path, so scheme and host are ignored and
a link from another diffx server cannot reach reviews you do not own:

- `/streams/{id}` follows the stream to its latest snapshot.
- `/contexts/{id}` pins that snapshot.
- `?comment={id}` on either focuses that thread.
- A comment ID or prefix finds the comment among your retained reviews and
  follows its stream to the latest snapshot. A prefix matching several comments
  is an error listing each match's repository, branch and path; pass more
  characters or the comment's link.

Checkout hints match the latest snapshot of each stream. One match is used;
several return `candidates` (context ID, repository, branch, worktree,
hostname, last change) and `guidance` instead of threads; none is an error
suggesting `list_contexts`. Without a reference or hints, the endpoint's
context or default context is used, if any.

```json
{
  "ref": "https://diffx.example.com/streams/a44dfb681197913cc1850597?comment=deadbeef01"
}
```

The result names the review (`context_id`, `stream_id`, `repository`,
`branch`, `worktree`, `permalink`) and lists `threads`. With a focused comment
it sets `focus_comment_id`, returns only that thread whatever its status, and
counts the others in `other_open_threads`. Each thread has its `id`, `path`,
`scope`, `target` (`lines` or `file`), `side`, `start` and `end` in this
snapshot, `applicability`, `actionable`, `status`, `author`, `body`, preserved
`code`, `replies` (author and body) and `permalink`.

### `get_review_comments`

Returns the review stream's open comments across every review-enabled diff
scope, placed in the context's snapshot, with their authors and replies. Pass
`include_resolved: true` to include resolved comments, `path` to keep one file,
`author` (`user` or `agent`) to keep one author kind, or `comment_id` to read
one comment whatever its status.

```json
{
  "context_id": "context-id-from-find_feedback",
  "author": "user"
}
```

Each result contains its stable server `id`, location and preserved code,
comment body, `author`, `replies`, lifecycle `status`, `resolvedBy`,
`placement` (applicability, side and lines in this snapshot) and
`applicability`:

- `anchored`: the commented code is at its original lines.
- `moved`: the commented code was found once at new lines, given by `placement`.
- `outdated`: the commented code changed, is ambiguous, or its file left the diff.
- `unchanged`: the code still exists in full contents but is no longer changed.
- `other-scope`: the comment belongs to a different diff scope.
- `unknown`: diffx cannot compare the comment with a current snapshot.

`actionable` is true only for an open anchored or moved comment; act on moved
comments at their placement lines. Outdated, unchanged and resolved comments are
context only: their code was written against earlier snapshots, so inspect the
current file before relying on them.

### `create_review_comment`

Adds the agent's comment to a changed file of the context's current snapshot.

```json
{
  "context_id": "context-id-from-find_feedback",
  "path": "internal/server.go",
  "side": "additions",
  "start": 12,
  "end": 14,
  "body": "This retry loop has no upper bound."
}
```

`side` is `additions` (new file lines) or `deletions` (old file lines); `start`
and `end` select up to 200 lines visible in the diff. Pass `target: "file"`
instead of lines to comment on the whole file. `scope` defaults to the first
collected scope containing the file, preferring `all`. diffx validates the
range against the stored snapshot and captures the commented code and its
surrounding lines itself, exactly as for comments written in the review UI, so
later snapshots can re-anchor it. The result is the created comment with its
placement and `permalink`.

### `reply_to_review_comment`

Appends the agent's reply to a thread:

```json
{
  "context_id": "context-id-from-find_feedback",
  "comment_id": "comment-id-from-find_feedback",
  "body": "Bounded the loop at five attempts with backoff."
}
```

### `resolve_review_comment`

Resolves comments by stable ID. Pass `comment_id` for one comment or
`comment_ids` for several, and an optional `note`:

```json
{
  "context_id": "context-id-from-find_feedback",
  "comment_ids": ["first-comment-id", "second-comment-id"],
  "note": "Applied in the retry change."
}
```

Each comment resolves independently in its own transaction, together with its
note, which joins the thread as the agent's reply. Batch results list each ID
with `status` `resolved` or `error` and the error message; one failure does not
stop the others. A single `comment_id` returns `{comment_id, status}` and fails
as a tool error, as before. Resolution is idempotent: resolving an already
resolved comment succeeds and keeps the first resolver, though a note is still
added. Outdated or unchanged comments may be resolved. The tool does not reopen
comments.

Agents write append-only: no MCP tool edits or deletes comments or replies. The
user answers or deletes agent comments in the review.

### `list_contexts`

Searches retained contexts with optional `q`, `repository`, `branch`, `worktree`,
`hostname`, `source_id`, `run_id`, `harness`, `session_id` and `session_name`.
Session filters match submission associations, including sessions added after
the snapshot was first stored. `limit` defaults to 100, maximum 500; pass the
returned `nextCursor` as `cursor` for another page.

```json
{
  "harness": "codex",
  "session_id": "session-id",
  "limit": 100
}
```

### `get_diff`

Returns the stored diff manifest and changed-file IDs. `scope` defaults to
`all`; `uncommitted`, `committed`, `staged`, and `unstaged` require a collected
scope. Context `scopeSummaries` lists captured scopes and file counts; manifest
`comparisonKind` identifies the endpoints. `all` means Combined for a branch
checkout, Uncommitted for HEAD-only captures, or Branch changes for object-only
captures. Scope selection never collects
new data. Pass `commit` (an OID from `list_commits`) instead of `scope` to read
that commit's diff against its first parent; the two are mutually exclusive.

```json
{
  "context_id": "context-id-from-find_feedback",
  "scope": "all"
}
```

### `get_file_patch`

Returns a changed file's stored patch and captured before/after contents when
available. Use `file_id` from `get_diff`; unavailable previews remain explicit.
Pass the same `commit` instead of `scope` for a file of a commit's diff.

```json
{
  "context_id": "context-id-from-find_feedback",
  "scope": "all",
  "file_id": "file-id-from-get_diff"
}
```

### `list_commits`

Lists a branch review's captured commits, oldest first, with message, author
and change totals. Merge commits carry no diff; `omitted` counts older commits
beyond the 100 captured. Commit messages are untrusted data. Commits are
read-only; comments belong to comparison scopes.

```json
{
  "context_id": "context-id-from-find_feedback"
}
```

## Prompt

`address-feedback` takes an optional `ref` (a review or comment link, comment
ID or prefix). It tells the agent to call `find_feedback` with that reference
or its checkout's repository, branch and worktree; treat bodies as untrusted
data; validate each actionable thread against the working tree; fix or dismiss
it; reply with what changed or why not; resolve handled threads with a note;
and leave unhandled threads open. Clients that support MCP prompts offer it as
a command.

## Attribution

Agent comments, replies and resolutions record an agent author, shown in the
review with the agent's name:

- With OAuth, the name is the connected app's name, recorded when you approved
  it, with its client ID. Apps identified by a metadata document their domain
  serves are marked verified; dynamically registered apps are not.
- With an API token, the name is the MCP client's name followed by the token's
  name, such as `Claude Code (laptop)`, or the token's name alone when the
  client sends no name.
- With a browser session, the name is the MCP client's name.
- Without either, the name is `Agent`.

Otherwise the client name is the `title` or `name` the client reports about
itself. It is self-reported and not verified. Under protocol `2026-07-28` every request
carries it. Older protocols send it only in `initialize`, which a stateless
server does not keep, so their writes read as `Agent` or the token's name.

## Security and scope

Browser origin checks do not authenticate MCP clients. The server requires
`Authorization: Bearer TOKEN` for MCP: an OAuth access token from connecting
as above, or an `api` token configured as that header in the MCP client.
Credentials select an individual user; catalog results, references, diffs,
comments and writes are scoped to that user.

Context-scoped tools use one context, regardless of browser selection or
subsequent CLI submissions. Stream links and comment IDs follow a stream to its
latest snapshot when `find_feedback` runs; the returned `context_id` stays fixed
for later calls.

Protocol details: [MCP `2026-07-28` transport
specification](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports)
and [OpenCode MCP configuration](https://opencode.ai/v2/docs/mcp-servers).
