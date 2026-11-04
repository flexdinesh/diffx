# Experimental native desktop

`diffx-native [PATH]` is a MyGo native client for the existing local composition.
It renders Go content views without webviews, HTML or JavaScript. macOS and
Linux amd64/arm64 are the initial build targets; Windows is deferred. Existing
web inprocess and distributed entry points remain available.

Install separately with the shell script:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-native.sh | sh
diffx-native /path/to/checkout
git diff | diffx-native
```

The default channel is experimental edge. See [release.md](release.md) for
version selection, platform bundles, notices, upgrades and removal. The installer
never overwrites `diffx`. App and matching private server install as one pair.

## Server and state

Native starts its bundled CLI automatically and talks to the existing public
REST/SSE contracts over the owner-only Unix socket. The server retains Git,
SQLite, immutable observations, review state, comment anchors and export.

A compatible server with the requested canonical scope is shared. Attaching
does not transfer ownership: native Quit preserves an attached server and stops
only one native started. Changing the running scope requires explicit
replacement. The loopback viewer remains available to web clients. Native
preferences are separate; comments and reviewed-file marks share server state.
Mutable review reads refresh every three seconds without collecting Git or
replacing the selected snapshot.

## Local workflow parity

| Workflow              | Native behavior                                                                                                                                                                  |
| --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Discovery and history | Repository, branch and worktree navigation; activity ordering; historical search, filters and pagination; captured stdin reviews                                                 |
| Comparisons           | Captured scopes, immutable snapshots, read-only commit browsing and stepping                                                                                                     |
| Files                 | Tree/list, filtering, collapse, resizable panes, scroll synchronization and keyboard navigation                                                                                  |
| Diff                  | Stored patch runs and old/new coordinates, split/unified, wrapping, syntax, word/character detail, EOF indicators, verified context expansion                                    |
| Comments              | File/range drafts, acknowledged save/retry, edit/delete/resolve/reopen/carry, moved/outdated placement, cross-scope navigation, bulk controls and canonical XML clipboard export |
| Review progress       | Shared reviewed marks, unchanged-patch projection, hide reviewed, reset and acknowledged advancement                                                                             |
| Recovery              | Collection/partial failures/Retry, explicit Sync, newer/unread notices, SSE recovery, stale-response cancellation and draft preservation across navigation/session replacement   |
| Appearance            | Generated canonical application themes and palettes, five code-theme choices, font/tab/display settings and native menus/dialogs                                                 |

Comments appear on code only in their captured scope. Cross-scope sidebar
comments show their scope; navigation switches comparison and waits for the
target preview. Commit views permit text copying but no review mutations.

## Interaction compromises

- Range selection uses click/Shift-click and keyboard controls; gutter dragging
  is unavailable through the current MyGo API. Stored ranges remain contiguous,
  side-specific and limited to 200 lines.
- Unified comments require selecting a line/range before using its gutter action.
- Native font geometry, controls and syntax categories differ from web. Themes
  derive from canonical tokens and licensed source palettes.
- Vertical context/comment changes preserve measured stored-line anchors in the
  outer file list. Horizontal offsets cannot be restored after editor recreation
  through the current public API.
- Alt+M marks and advances after acknowledgement. Pending repeats are ignored;
  holding the chord can mark successive files. It never unreviews an already
  reviewed file. The Reviewed control toggles existing marks.

## Verification

```sh
mise run check:native          # headless views, controller, real socket lifecycle
mise run check:native:window   # actual Linux GTK windows, focus, clipboard, sharing
mise run check:native:package  # actual four-target archives and version pairing
mise run native:platform-smoke # actual logged-in macOS/Linux desktop
```

These supplement the existing web, distributed, contract and release checks.
All three native groups participate in clean-HEAD `check:pr`. Test automation
uses a dedicated build tag; release builds exclude automation and inspector code.
The platform smoke is an explicit host supplement for actual OS behavior.
Cross-compilation does not establish macOS runtime acceptance. A Mac must run
the platform smoke before claiming that verification. No RAM or performance
measurement is part of this port.

Ownership and generated-source boundaries are documented in
[architecture.md](architecture.md) and [frontend.md](frontend.md).
