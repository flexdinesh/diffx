# Desktop webview

`diffx-desktop [PATH]` is a MyGo webview host for the existing local composition.
Browser and desktop entries mount `ReviewApplication({ platform })` from
`packages/review-app`. Review views, state, styles, themes, workers and Pierre
rendering have one owner; `packages/ui` owns tokens and primitives. macOS and
Linux amd64/arm64 are build targets. Windows is deferred.

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-desktop.sh | sh
diffx-desktop /path/to/checkout
git diff | diffx-desktop
```

The installer defaults to experimental edge. It verifies checksums and atomically
selects the app plus matching private CLI without replacing `diffx`. See
[release.md](release.md) for version selection, upgrades, licensing and removal.

## Host boundary

`apps/desktop` remains an independent Go module. Its session adapter executes
the matching CLI and consumes `packages/localprotocol`. The thin host serves
embedded frontend assets and proxies allowlisted public REST/SSE through the
local sidecar viewer on one loopback origin. It never proxies private control
endpoints or owns Git, SQLite, observations, review rules or export formatting.

The typed bridge exposes launch state, folder selection, retry and explicit
scope replacement. OS menus and dialogs remain native. A compatible existing
local scope can be attached; another scope needs explicit replacement. Quit
stops only an owned server. PATH/stdin, folder launch and browser coexistence
retain the public local-session contract.

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
