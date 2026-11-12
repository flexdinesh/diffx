# Desktop webview

`diffx [PATH]` opens the MyGo desktop by default when a matching desktop pair is
installed. `diffx-desktop [PATH]` starts the host directly. Both use the personal
SQLite composition; `diffx connect` also accepts pinned TLS invitations and SSH.
Browser and desktop entries mount `ReviewApplication({ platform })` from
`packages/review-app`. Review views, state, styles, themes, workers and Pierre
rendering have one owner; `packages/ui` owns tokens and primitives. macOS and
Linux amd64/arm64 are build targets. Windows is deferred.

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-desktop.sh | sh
diffx /path/to/checkout
git diff | diffx
diffx connect ssh://devbox
```

The installer defaults to the latest stable release; pin one with
`--version vX.Y.Z`. It verifies checksums and atomically
selects the app plus matching CLI through one active pointer, exposing both
`diffx` and `diffx-desktop` and replacing existing command launchers. See
[release.md](release.md) for version selection, upgrades, licensing and removal.

## Host boundary

Native handoffs carry the invoking command's personal configuration, Git/SSH
environment and working directory to its sidecar, without changing the running
webview's environment. Piped captures are capped at 16 MiB while spooling and
remain cancellable even when an inherited producer keeps stdin open.

`apps/desktop` remains an independent Go module. Its session adapter executes
the matching CLI and consumes `packages/localprotocol`. The thin host serves
embedded frontend assets and proxies the shared public review REST/SSE allowlist
through native Unix, pinned TLS or SSH-forwarded loopback connections on one
loopback origin. The default local sidecar is socket-only; explicit `--port`
adds a separate loopback viewer. `--browser` opens a foreground browser viewer;
`--no-browser` serves it without opening a client, and `--headless` serves only
the socket in the foreground. It never proxies private control
endpoints or owns Git, SQLite, observations, review rules or export formatting.

The CLI returns after desktop acceptance over owner-only IPC, before server
readiness. Failure before acceptance allows browser fallback; failures after
acceptance remain visible in the window. One window focuses the same target;
a changed workspace or connection requires confirmation. Cancel preserves the
current session; accepted changes cancel and join prior requests and streams.
The typed bridge exposes launch state, folder selection, retry and explicit
scope replacement. OS menus and dialogs remain native. A compatible existing
local scope can be attached; another scope needs explicit replacement. Quit
stops only an owned server. PATH/stdin, folder launch and browser coexistence
retain the public local-session contract. Remote leases never stop their server.

`diffx remote [PATH]` runs the personal server in the foreground with pinned TLS
on `:7981` by default (`--listen` and `--advertise HOST:PORT` override it). It
prints a one-use `diffx1_` invitation valid for ten minutes. `diffx connect STRING`
pairs and opens desktop; native owner-only credentials allow reconnect during
that server run. `diffx remote pair` rotates unused invitations while retaining
paired credentials; restart requires a new invitation. Credentials never reach
React, browser URLs or child argv. `connect --browser`, `--no-browser` and
`--json` use a filtered loopback gateway and stay foreground. TLS and both
gateways allow review/collection routes, never private control, MCP, accounts
or ingestion. Remote reconnect backoff caps at 15 seconds for two minutes, then
offers Retry, without replaying mutations. Distributed-account login, connection
entry/switcher UI, SSH discovery and SSH auto-start remain deferred.

Desktop starts with web preferences. There is no migration from old native
settings. Appearance and display preferences persist across restarts: the host
retains its loopback port to keep the webview origin stable. An occupied saved
port causes an explicit startup error; it never silently changes the origin.
Desktop and external-browser preferences are separate; server comments,
reviewed marks and review data remain available. Old native installations have
no compatibility aliases and may be removed separately without deleting data.

## Runtime requirements and verification

The shared build targets Safari 16.4 or equivalent WebKit. Linux requires GTK3
and WebKitGTK 4.1 (WebKit 2.40+); macOS requires 13.3+. The host enforces these
version gates; acceptance on the minimum versions is required before calling
them tested. Unsupported older WebKit runtimes are outside the supported
target. The real Linux window suite must establish shared review behavior,
workers, rendering, keyboard, clipboard, recovery and lifecycle in WebKitGTK;
Chromium coverage alone is insufficient. macOS runtime acceptance requires a Mac.
Linux execution and cross-compilation do not establish macOS runtime acceptance.

```sh
mise run desktop:build
mise run desktop:dev
mise run check:desktop
mise run check:desktop:window
mise run check:desktop:package
mise run desktop:platform-smoke
```

The three desktop groups participate in clean-HEAD `check:pr` alongside browser
local/remote regressions. Packaging verifies four-target archives, matching
versions, notices and actual embedded desktop/browser assets. Production requires
`mygo_noinspector` and excludes automation. Acceptance uses `desktopautomation`.
Node and pnpm are build dependencies only; installed apps use system WebKit.

See [architecture.md](architecture.md), [frontend.md](frontend.md) and
[validation.md](validation.md) for ownership, build staging and isolation.
