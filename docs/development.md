# Development

Requires [mise](https://mise.jdx.dev) 2026.8.6 or later, Git, Bash, a C
compiler, and Docker (Linux x64) for isolated checks.

## Install from main

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-edge.sh | sh
diffx --version  # confirm installed build
```

Installs the rolling edge CLI into `~/.local/bin`; ensure it is on `PATH`.
Use `install.sh` for stable releases; see [release.md](release.md) for options.
Go source installs (`@latest`, `@edge`, `@<sha>` and branch refs) are unsupported
because source refs lack embedded web assets.

After checkout setup, install your current changes:

```sh
mise run install  # builds embedded assets, installs into $GOBIN or $GOPATH/bin
```

## Setup

```sh
mise trust && mise install && mise run setup  # tools, deps, hooks, Chromium, staged assets
mise run check:engine:setup                   # once per machine, isolated checks
mise tasks                                    # list all tasks
```

## Build

```sh
mise run build                              # dist/diffx, dist/diffx-server, dist/diffx-console
mise run --force build                      # rebuild despite unchanged sources
mise run web:build                          # web app and console only
mise run web:stage && mise run console:stage  # embedded assets for go build/test
mise run generate                           # API types, SQL queries, plugin adapters
mise run plugins:build                      # agent plugin adapters
```

## Frontend

The experimental desktop webview app has its own Go module and pinned MyGo tool:

```sh
mise run desktop:build
apps/desktop/build/dev/diffx-desktop /path/to/checkout
mise run desktop:dev
mise run check:desktop
mise run check:desktop:window
mise run check:desktop:package
mise run desktop:platform-smoke  # actual macOS/Linux desktop session
```

The build stages `@diffx/desktop` into `apps/desktop/internal/assets/dist`,
then places a matching private `diffx` beside `diffx-desktop`. Development passes
that sidecar's absolute path through `DIFFX_DESKTOP_SIDECAR`; the app never
searches PATH for another server. `desktop:frontend` builds the Vite entry;
`desktop:stage` runs the staging helper before raw desktop Go builds or tests.
Node is a build dependency; the installed app uses system WebKit, not Node.
Browser and desktop use the same `packages/review-app` implementation.

Desktop checks use the managed Dagger engine. They cover the host and typed
bridge, real Unix-socket sessions, Linux WebKitGTK windows under Xvfb, and four
target archives with embedded frontend verification. `desktop:platform-smoke`
supplements isolation on a real macOS or Linux session. Acceptance builds use
`desktopautomation`; production excludes automation and inspector. macOS
runtime acceptance requires a Mac. See [desktop-webview.md](desktop-webview.md).

```sh
mise run dev                                    # Vite HMR + fixture server
mise run dev -- --port 5181 --strictPort        # distinct port per worktree
mise run dev:server -- --no-browser             # fixture server, built assets
mise run dev:server -- --port 0 --no-browser    # fixture server, OS-picked port
pnpm --filter @diffx/ui dev                     # design gallery at /design.html
mise run docs:dev                               # docs site at localhost:5173
```

## Local experiments

```sh
./dist/diffx                                   # desktop; returns after acceptance
./dist/diffx . --browser                       # foreground browser viewer
./dist/diffx . --no-browser                    # foreground viewer, print URL
./dist/diffx . --port 7995                     # desktop plus fixed viewer port
./dist/diffx . --headless                      # socket only, no viewer
./dist/diffx . --state memory                  # no persisted state
./dist/diffx . --replace                       # replace running instance
./dist/diffx . --debug                         # collection progress
git diff | ./dist/diffx                        # piped patch
./dist/diffx dev --fixture test/fixtures/sample.diff --state memory  # fixture
./dist/diffx status --json                     # running instance
env -u DIFFX_CONFIG_PATH -u DIFFX_LOCAL_STATE DIFFX_RUNTIME_DIR="$(mktemp -d)" ./dist/diffx . --port 0 --no-browser  # isolated session
```

Desktop requires a matching pair from `install-desktop.sh` or `desktop:build`.
A CLI-only install falls back to foreground browser serving when desktop cannot
accept the launch. A desktop session otherwise needs no viewer port. Use explicit
`--browser`, `--no-browser` or `--headless` for foreground experiments.

## Personal TLS server

```sh
./dist/diffx remote ~/work                     # foreground TLS on :7981
./dist/diffx remote ~/work --listen :8443 --advertise devbox:8443
./dist/diffx remote pair --advertise devbox:8443 # rotate unused invitation
./dist/diffx connect STRING                    # copy printed diffx1_ invitation
./dist/diffx connect STRING --browser          # foreground loopback gateway
./dist/diffx remote -- status                   # directory named status
```

Invitations are one-use, expire after ten minutes and pin a per-run TLS identity.
Owner-only native credentials allow reconnect using the original command during
that run; restarting requires a fresh invitation. No external address discovery
runs. The listener uses the same personal SQLite/runtime singleton, with explicit
network review/collection policy and no private control, MCP, accounts or ingestion.
`diffx remote status` still checks the distributed sync destination below;
pairing state does not use `remote.url` or `remote.token`.

## Distributed server

```sh
mise run dev:remote                  # diffx-server + dev Postgres + Vite at localhost:5173
mise run dev:console                 # admin console at localhost:5174
mise run db:up                       # dev Postgres only (127.0.0.1:54329)
mise run db:down                     # stop, keep data
mise run db:reset                    # wipe and restart
DIFFX_SERVER_DATABASE_URL=postgres://diffx:diffx@127.0.0.1:54329/diffx?sslmode=disable DIFFX_SERVER_INSECURE_COOKIES=true ./dist/diffx-server  # built binary
mise run docker:up                   # container server + bundled Postgres at 127.0.0.1:7980
DIFFX_SERVER_PORT=7979 mise run docker:up  # other host port
docker compose logs server           # container logs
mise run docker:down                 # stop, keep data
diffx-console user reset-password --email alice@example.com  # offline reset link
```

Sync to a dev server (token from Settings):

```sh
DIFFX_REMOTE_URL=http://127.0.0.1:7980 DIFFX_REMOTE_TOKEN=... ./dist/diffx remote status
DIFFX_REMOTE_URL=http://127.0.0.1:7980 DIFFX_REMOTE_TOKEN=... ./dist/diffx sync --debug
```

## SSH

```sh
ssh devbox 'diffx ~/work --headless'                  # start remote (use tmux)
./dist/diffx connect ssh://devbox                     # forward + desktop
./dist/diffx connect ssh://devbox --browser           # foreground browser gateway
./dist/diffx connect ssh://dee@devbox:2222 --port 7995 --no-browser
./dist/diffx connect ssh://devbox --remote-diffx ~/go/bin/diffx  # remote binary path
./dist/diffx connect ssh://devbox --json              # foreground gateway URL as JSON
ssh devbox diffx status --json                        # remote socket and viewer
ssh -N -L 127.0.0.1:7991:/run/user/1000/diffx/diffx.sock devbox  # manual forward
```

SSH still uses system aliases, agents and jump hosts. Browser/JSON connections
expose a filtered gateway; a manual raw socket forward also carries private
control routes. The remote headless server remains running after disconnect.

## MCP

```sh
./dist/diffx mcp  # stdio bridge to running instance
```

## Checks

```sh
mise run check:static       # types, lint, format, tracked build outputs (host)
mise run check:artifacts    # tracked application binaries and web build outputs (host)
mise run check:go           # Go tests + vet
mise run check:unit         # web/console unit, DOM, plugins
mise run check:browser      # Playwright
mise run check:contracts    # boundaries, design tokens, shared rules
mise run check:boundaries   # package dependencies (host)
mise run check:design       # design-token use (host)
mise run check:conformance  # API contract
mise run check:docs         # docs site
mise run check:generated    # generated files up to date
mise run check:release      # release tooling
mise run check              # everything, working files
mise run check:pr           # clean HEAD, before PR
mise run check:pr --force   # rerun, ignore cached results
mise run check:status       # results for HEAD
pnpm format                 # format
pnpm lint                   # lint
pnpm typecheck              # types
pnpm test:browser:install   # Playwright Chromium
DIFFX_TEST_DATABASE_URL=postgres://diffx:diffx@127.0.0.1:54329/diffx?sslmode=disable go test ./...  # host Go tests on dev Postgres
```

## Diagnostics

```sh
diffx diagnostics export --latest --output failure.json
diffx diagnostics export REPORT_ID --output failure.json
```

## More

- [validation.md](validation.md): toolchain, builds, isolated checks
- [local.md](local.md): local sessions, state, collection
- [server.md](server.md): server and console configuration
- [frontend.md](frontend.md): frontend conventions
- [release.md](release.md): releases and edge
- [architecture.md](architecture.md), [system.md](system.md)
