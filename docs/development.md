# Development

Requires [mise](https://mise.jdx.dev) 2026.8.6 or later, Git, Bash, a C
compiler, and Docker (Linux x64) for isolated checks.

## Install from main

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@edge                 # latest main (edge channel)
GOPROXY=direct go install github.com/flexdinesh/diffx/cmd/diffx@edge  # bypass proxy cache
mise run install                                                       # current checkout into $GOBIN
diffx --version                                                        # confirm installed build
```

`@main`, `@<sha>` and feature branches do not build; use `@edge` or a checkout.

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
./dist/diffx                                   # review cwd, opens browser
./dist/diffx . --no-browser                    # print URL only
./dist/diffx . --port 7995                     # fixed viewer port
./dist/diffx . --headless                      # socket only, no viewer
./dist/diffx . --state memory                  # no persisted state
./dist/diffx . --replace                       # replace running instance
./dist/diffx . --debug                         # collection progress
git diff | ./dist/diffx                        # piped patch
./dist/diffx dev --fixture test/fixtures/sample.diff --state memory  # fixture
./dist/diffx status --json                     # running instance
env -u DIFFX_CONFIG_PATH -u DIFFX_LOCAL_STATE DIFFX_RUNTIME_DIR="$(mktemp -d)" ./dist/diffx . --port 0 --no-browser  # isolated session
```

## Remote server

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
./dist/diffx connect ssh://devbox                     # forward + open browser
./dist/diffx connect ssh://dee@devbox:2222 --port 7995 --no-browser
./dist/diffx connect ssh://devbox --remote-diffx ~/go/bin/diffx  # remote binary path
./dist/diffx connect ssh://devbox --json              # print forward URL as JSON
ssh devbox diffx status --json                        # remote socket and viewer
ssh -N -L 127.0.0.1:7991:/run/user/1000/diffx/diffx.sock devbox  # manual forward
```

## MCP

```sh
./dist/diffx mcp  # stdio bridge to running instance
```

## Checks

```sh
mise run check:static       # types, lint, format (host)
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
