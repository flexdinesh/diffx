# Validation and builds

diffx is a Go server with a React/Vite web application embedded into the
release binary. Node and pnpm are build and test dependencies only. This covers
toolchain, build, test and isolated-check internals; commands live in
[development.md](development.md).

## Toolchain

Mise manages Go 1.27, Node from `.node-version`, and pnpm from `package.json`.
`mise run setup` installs dependencies, hooks, and Playwright Chromium.

`mise run` activates project tools without shell activation. For direct pnpm
commands, activate mise in your shell or use `mise exec -- pnpm <script>`.
List available tasks with `mise tasks`. Keep personal overrides in
`mise.local.toml`, which is ignored by Git.

`mise.lock` pins tool checksums for Linux x64 and macOS arm64; CI installs
with `--locked`. After changing a tool pin, including task-level tools, run
`mise lock` and commit the lockfile; `mise lock --bump` moves pins to the newest
allowed versions. `min_version` in `mise.toml` must match the mise version CI
installs and the requirement in [development.md](development.md); a contract test enforces this and that the
Dockerfile Go image matches the mise Go pin. `GOTOOLCHAIN=local` makes Go use
the mise pin instead of downloading a toolchain, so a `go.mod` bump past it
fails until `mise.toml` is updated. `mise run deps` installs pnpm dependencies
from the lockfile.

Build tasks declare `sources` and `outputs`; mise skips `web:build`,
`web:stage`, `build`, `plugins:build` and `docs:check` when their source
contents are unchanged. Run `mise run --force <task>` to rebuild anyway. CI checkouts always
build.

## Test services

Server-mode Go tests need Docker: `internal/testsupport/pgtest` starts one
pinned `postgres:18-alpine` container per test process (removed by
testcontainers' reaper) and gives every test its own database cloned from a
template migrated once per schema. Set `DIFFX_TEST_DATABASE_URL` to a
`postgres://` URL whose role may create databases to use a running server
instead, such as the development Postgres. Local-mode tests stay on SQLite.
Direct `go test` commands run on the host. Public mise test tasks supply a
private Postgres service through Dagger and do not inherit a host
`DIFFX_TEST_DATABASE_URL`.

Browser tests reuse the full check's web, CLI and `diffx-server` builds;
standalone browser runs build them when needed. Chromium uses one worker with an
in-memory fixture server behind a Vite preview on a free port. The remote spec
(`remote-server.spec.ts`) instead starts the worker's own `diffx-server` on a
fresh Postgres database, like the Go tests (Docker, or `DIFFX_TEST_DATABASE_URL`),
serves its embedded web build at the server's origin, and syncs with the CLI
without API mocks.

## Isolated local validation

Provision the repository's engine explicitly, once per machine:

```sh
mise run check:engine:setup
```

Dagger CLI, Go SDK and engine are pinned to 0.21.11. The engine is named
`diffx-validation-engine-v0.21.11` and uses its own named cache volume. Setup
inspects ownership before creating anything; it never replaces a foreign or
ambiguously owned engine. Check commands connect directly to this engine and
fail if it is unavailable, rather than provisioning one automatically.
`DAGGER_LEAVE_OLD_ENGINE=true` is set before initialization to prevent cleanup of
other agents' engines. It does not protect this engine from another agent's
cleanup, which is why its name is outside the `dagger-engine-*` prefix.

Never stop, restart, remove or prune another task's engine or volumes. Engine
upgrades and cleanup are explicit maintenance operations requiring coordination
with active worktrees. Do not run broad Docker or Dagger prune commands.
Cancellation closes only the current validation session; the shared engine and
its cache persist.

`mise run check` validates a snapshot of working files, including
non-ignored untracked files. It does not certify committed HEAD. Before creating
or updating a PR, commit the intended changes and run:

```sh
mise run check:pr
mise run check:status
```

PR validation requires clean tracked files and no non-ignored untracked files.
It validates captured HEAD and rechecks the checkout before reporting readiness.
Run `check:status` immediately before PR creation/update and confirm HEAD has not
changed since validation. PR creation remains separate. There is no full-suite pre-push hook;
`mise run check:push` remains an alias for explicit PR validation. Agents must run `check:pr` before creating or updating a PR. Local
commands and validation records are bypassable and are not remote enforcement.

Use `mise run check:<group>` for targeted checks. Groups are `static`,
`contracts`, `go`, `unit`, `browser`, `conformance`, `docs`, `release`, and
`generated`. They cover the existing static checks, architecture guards, Go
tests, web/console unit and DOM tests, plugins, browser suites, API contracts,
docs build, release tooling, and generated API/SQL/plugin consistency. PR
validation uses Go's race detector. Public mise `test:*` tasks route through
these isolated groups. mise is the only task runner: package.json scripts wrap
JS tooling (build, typecheck, lint, format, raw test runners) and never call
mise. Groups invoke those scripts and hidden mise `exec` tasks inside
containers; running them directly, like direct `go test`, bypasses isolation.

The `static` group runs on the host: type checking, lint, format verification,
Go formatting and read-only architecture/design guards. It installs locked
dependencies into a disposable source snapshot, with its own temporary directory;
it never writes build outputs or dependencies into the working tree. It needs no
Docker or Dagger CLI. Installed Go, Node and pnpm binary digests participate in
its success identity. Development servers, formatting and direct static commands
also stay on the host. Go vet runs with the isolated Go group because it needs
embedded application build assets. Docs builds and generated-file verification
stay in Dagger because they produce build outputs or regenerate source files.

Each executing container group has private writable files, home, temporary directories and
test services. Browsers and fixture servers run together inside the container;
localhost refers to that execution, not another worktree or a host service.
Checks publish no application ports, use no host networking or tunnels, and
mount no host Docker socket. Existing per-test database isolation still applies.
Prepared dependencies and application builds are reused as immutable inputs.

Successful group results are recorded atomically in the user cache and reused
across this repository's worktrees when input contents, paths, modes,
configuration, toolchain, platform and runner implementation match. Branch names,
commit IDs, elapsed time and small diffs do not establish equivalence. Unknown
inputs invalidate conservatively. A history-only rebase may reuse results;
upstream content changes invalidate affected groups. Failed or cancelled runs
never produce successful validation records.

To execute checks again while retaining reusable preparation/build work:

```sh
mise run check:pr --force
mise run check:browser --force
```

This bypasses successful validation records and underlying test-result caches.
It is different from mise's own `mise run --force <task>` freshness override.

Groups run sequentially, with Go limited to two concurrent packages and
`GOMAXPROCS=2`, one Playwright worker and one pnpm workspace worker. A shared
user-level admission mechanism permits two participating invocations across
worktrees and repositories. It does not constrain unrelated tools or impose a
hard memory limit.

Logs, browser traces and result metadata live in `dist/check-runs/<run-id>/`.
The latest 20 completed runs are retained; active runs are never pruned.
Ordinary failures retain artifacts and return nonzero. Cancellation preserves
streamed logs and completed artifacts; a browser killed mid-trace may not leave
a usable trace. Results distinguish executed checks from reused validation.

Validation containers currently support Linux x64 only. They do not validate
native macOS behavior; direct native checks remain optional. `mise run setup`
still installs host dependencies, Playwright Chromium and staged assets for
development. Reinstall Chromium after upgrading Playwright.

GitHub Actions does not run PR checks. Run `check:pr` and `check:status` locally
before creating or updating a PR, including after rebasing onto changed upstream
content. `main` requires no hosted validation status. This workflow relies on developer and
agent compliance; GitHub does not enforce local validation.

Hosted workflows publish edge builds on pushes to `main` and stable releases on
manual dispatch. They stage embedded assets and retain release compilation,
checksums and Homebrew formula syntax validation without repeating the general
check suite. Deployment workflows remain separate.

## Embedded assets

`mise run install` embeds the current web build and installs `diffx` into
`$GOBIN`, or `$GOPATH/bin` when `GOBIN` is unset. Ensure that directory is on
`PATH`.

The production web build is staged into `internal/webui/dist` and the console
build into `internal/consoleui/dist`; both are ignored on `main`. Go builds,
tests and vet embed them, so run `mise run web:stage` and
`mise run console:stage` (or `mise run build`) after a fresh checkout or
frontend change; without them Go fails with
`pattern all:dist: no matching files found`. CI commits the web assets only on
release tags and the `edge` branch; see [release.md](release.md). The console
ships only as a container, built by the deploy pipeline.

## Build pipeline

The production build generates API types, builds the web application, stages the
Vite output under `internal/webui`, and embeds those assets into the Go binary.
The resulting `dist/diffx` and `dist/diffx-server` executables have no
Node runtime dependency. Git is required only for producer-side checkout
collection. Local queries read SQLite and server queries read Postgres; neither invokes Git.

The OpenAPI contract in `packages/api/openapi.yaml` is the frontend/server
boundary. After changing it, run:

```sh
mise run generate
```
