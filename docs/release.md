# Releases

## Stable releases

Only the manual **Release** workflow creates stable version tags and GitHub
Releases. It checks out the latest `main` when the run starts and marks the
release as GitHub's latest stable release. Dispatches from other branches are
skipped. Releases publish downloadable binaries; they never deploy servers.

Checks and test suites run locally through explicit `mise run check:pr` before
creating or updating PRs, including after rebasing onto changed upstream content.
GitHub Actions does not validate PRs.
Workflows set `HUSKY=0`. Publishing stages embedded web assets in the workflow
workspace, compiles release binaries with GoReleaser, and generates checksums.
It does not repeat lint, type checks, generated-file verification or test suites.
Release steps run through `release:*` mise tasks. Stable and edge publishing share
`mise run release:package`, which installs GoReleaser and builds archives without
publishing. The channel tasks upload those archives to GitHub.

Install the latest stable release or a specific version:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh -s -- --version v0.2.0
```

Both stable and edge installers support `--bin-dir DIR`, defaulting to
`~/.local/bin`. They install only `diffx`, verify SHA-256 using `checksums.txt`,
and replace the binary atomically after verification. A failed download or
checksum preserves an existing binary. No Go or Node.js is required, no sudo is
used, and shell profiles are unchanged. Ensure the destination is on `PATH`.
See [install options](../docs-website/src/pages/getting-started.md).

1. Merge release-ready code to `main`. When the release changes a protocol that
   servers must accept first, promote that commit to prod before continuing.
2. Run the **Release** workflow with `main` selected. It requires no inputs.
3. The workflow tags that `main` commit directly, stages the embedded web assets
   locally, and publishes the GitHub Release with GoReleaser. It commits no
   build outputs.

Each release contains checksums plus Linux and macOS archives for amd64 and
arm64; Windows is not supported. Archives include the native binary, README,
and license. The embedded web application needs no installed Node.js
runtime.

Stop running binaries before upgrading. Local schema 11 through 15 state upgrades in place to
schema 16 and survives subsequent starts; older, future or unrecognized schemas are refused without
resetting data. Local `diffx` offers to back up older state beside it and start
fresh, or does so with `--reset-state`. Servers migrate their Postgres database
at startup and never reset it. Server schema 10 refuses older binaries, so stop
every older `diffx-server` and `diffx-console` before upgrading; it is not a
rolling upgrade. Upgrade server and collectors
together for ingestion protocol 4, deploying the server first (promote prod
before running **Release**): new producers send
optional activity metadata that older servers reject. Existing protocol 4
submissions without that metadata still replay unchanged and need no reset. In-memory state lasts only for one process.

Rerunning a release for the same `main` commit reuses its tag and existing
GitHub artifacts. Published stable artifacts are not rebuilt or replaced.
Version selection also recognizes historical tags whose asset commit's first
parent is that `main` commit.

Homebrew publishing is retired. The external tap and historical releases are
left untouched; shell installation is the supported distribution path.

## Development installs

Install the latest `main` commit published to edge, including unreleased changes:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-edge.sh | sh
```

On a push to `main`, the **Edge** workflow stages web assets locally and
publishes a rolling GitHub prerelease with tag `edge` pointing to that `main`
commit. It embeds the main SHA in the binary. Assets use fixed names
`diffx_edge_<os>_<arch>.tar.gz` plus `checksums.txt`. Edge omits macOS Intel
(`darwin_amd64`) for both CLI and desktop archives; its installers refuse that
platform before downloading. Stable releases keep it. Edge is excluded from
GitHub's latest stable release and from `install.sh`'s default selection.
The legacy `edge` branch is no longer updated.

Downloads can fail or encounter a checksum mismatch while rolling assets are
being replaced. Retry after publication finishes; the installer preserves any
existing binary on failure. Use `--bin-dir DIR` to override `~/.local/bin`.

Go source installs (`@latest`, `@edge`, `@<sha>` and branch refs) are unsupported
because source refs lack embedded assets. Historical binary releases remain
installable separately with `install.sh --version vX.Y.Z`. For development from
a configured checkout, use `mise run install`.

## Experimental desktop webview

Desktop archives retain a role/version/source identity in each executable.
Runtime version reporting and cross-target verification consume the same record;
verification does not depend on linker settings, which Go omits with `-trimpath`.
Host-compatible binaries also execute their informational version commands.

`diffx-desktop` is a separate macOS/Linux MyGo v0.7.4 webview host. It
embeds the shared React review app and uses a private matching CLI as its local server.
Normal `install.sh`, `install-edge.sh`, CLI assets and their `checksums.txt`
retain their existing behavior. Desktop assets use separate names and
`desktop-checksums.txt` on the same source release. Edge remains experimental;
desktop stable-version archives are also explicitly experimental.

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-desktop.sh | sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-desktop.sh | sh -s -- --version v0.2.0
```

The desktop installer defaults to edge and supports `--version edge|vX.Y.Z`
and `--bin-dir DIR` (default `~/.local/bin`). It verifies one exact SHA-256
entry before extraction, stages the app/server pair on the destination
filesystem, and exposes both `diffx` and `diffx-desktop`. Existing command
launchers, including a standalone CLI, are replaced with symlinks to
`.diffx-desktop-versions/current/bin/<command>`. A single atomic `current`
symlink switch selects the matching pair. A private install lock serializes
migration and activation. Pre-activation failure restores replaced launchers;
failed recovery retains backups and reports their location.
No downloaded program runs during installation. Shell profiles are unchanged.

Private pairs live in `.diffx-desktop-versions/install.*/payload/`; each install's
`bin/` links to its app and CLI, and `previous/` retains replaced launchers when
needed. `current` points to one `install.*` directory. Upgrades retain old pairs
so a running app can still start its matching server. Stop all desktop apps
before deleting inactive pairs. To uninstall the default pair, remove
`~/.local/bin/diffx`, `~/.local/bin/diffx-desktop` and
`~/.local/bin/.diffx-desktop-versions`; review data and preferences remain.
CLI-only installers remain available separately and replace only `diffx`.
Reinstall the desktop pair to restore both managed launchers.

`diffx [PATH]` defaults to desktop and returns after desktop acceptance; explicit
browser/no-browser/headless variants stay foreground. Desktop serves local
sessions over the socket unless `--port` requests a viewer. Both clients share
local database and lifecycle rules. Closing desktop stops only an owned local
server; attached and remote servers stay running. Local app/CLI builds and
contracts must match. Remote compatibility uses explicit connection/API protocol
versions, rather than requiring identical builds.

Desktop archives are `diffx-desktop_<version>_<os>_<arch>.tar.gz` for macOS
(`darwin`) and Linux, each amd64/arm64. macOS contains
`Diffx Desktop.app`, with the desktop executable in `Contents/MacOS`
and private `diffx` in `Contents/Resources`. Its bundle launcher passes
`--desktop-launch` to attach or choose a folder. Linux contains a
`diffx-desktop/` directory with sibling `diffx-desktop` and private `diffx`, plus
a desktop entry invoking `diffx-desktop --desktop-launch`. Put the installed
command on the desktop environment's PATH before registering that entry.
Shell invocation preserves local path/pipe arguments. Keep each pair together.

Artifacts include Diffx's license, collected linked Go/runtime and web dependency
notices and build provenance. The private CLI retains embedded browser assets
for web coexistence, with the licensing exceptions below. Linux requires the
system GTK3 and WebKitGTK 4.1 libraries; macOS uses system WebKit. The shared
frontend targets Safari 16.4 or equivalent. There is no DMG, package installer,
Developer ID signing, notarization or
auto-updater. macOS security policy may require explicit user approval for
the unsigned experimental bundle.

Build local working files without publishing:

```sh
mise run desktop:package
mise run desktop:package-verify
DIFFX_DESKTOP_TARGET=darwin/arm64 mise run desktop:package
```

`desktop:package` stages both desktop and browser assets and defaults to version `0.0.0-dev` and
the checked-out source SHA. Ignored outputs live in `apps/desktop/build/release/`.
`desktop:package-verify` additionally checks archive checksums, layouts, embedded frontend bytes, notices,
target metadata and version metadata, executing both `--version` commands
when the target matches the host. It opens no window.
For release builds, set `DIFFX_RELEASE_VERSION`, `DIFFX_SOURCE_SHA` and optionally
`DIFFX_DESKTOP_TARGET`, then run `mise run release:desktop-package` on clean
source. It uses `mygo_noinspector`, excludes automation, injects the same version
into app and server, and emits an archive plus `.sha256` sidecar. Local release
tests run through `mise run check:release`; the raw JS tooling command is
`pnpm test:release`.

Stable and edge workflows call **Desktop artifacts** after the CLI release
publishes, passing its exact source SHA and tag. That workflow only stages,
cross-compiles and uploads; it runs no test suite and never creates a release
or changes latest/prerelease status. CLI publication can succeed independently
of experimental desktop packaging. Desktop checksums upload last. Edge checks
that both current main and edge tag still match before uploading; rolling
asset replacement can temporarily fail checksum verification, so retry after
publication completes. Stable desktop uploads never clobber existing assets;
a complete rerun preserves them, while a partial upload requires maintainer
inspection before retrying. No release artifact is committed.

Cross-compilation and archive tests do not establish macOS/Linux window runtime
behavior. Run desktop platform acceptance separately before claiming support;
neither RAM nor performance measurements are required.

Former `diffx-native` installs are not aliases. After stopping the old app,
remove its launcher and `.diffx-native-versions` directory from the installation
bin directory. Do not delete server review data or user configuration files.
`install-desktop.sh` installs a new atomic pair in `.diffx-desktop-versions`;
old native settings are not migrated. macOS runtime acceptance remains a
separate Mac requirement.

## Version series

`.release-version` contains the active `major.minor` release series. It is
`0.2`, so the first release is `v0.2.0`; later releases automatically select
`v0.2.1`, `v0.2.2`, and so on. A rerun from the same commit reuses its existing
tag and release.

To begin a new minor or major series, change `.release-version` in the repo. For
example, changing it to `0.3` makes the next release `v0.3.0`; changing it to
`1.0` makes the next release `v1.0.0`. Later releases continue incrementing that
series' patch number.

## Embedded assets

Application binaries and web build outputs are never committed. Publishing
builds and stages `internal/webui/dist` only in its workspace before compiling
archives from the tagged main commit. It creates no asset commits. The CI
identity must be allowed to push version tags and update the rolling `edge` tag.

`mise run check:artifacts`, included in host static validation, rejects tracked
application binaries and web build outputs. Built plugin distributions and
generated API/SQL source remain committed.

Stage assets locally before Go builds and tests:

```sh
mise run web:stage
mise run console:stage
```

## Licensing

diffx is MIT licensed. Linked Go modules and runtime npm dependencies use
permissive licenses (MIT, BSD, ISC, Apache-2.0, 0BSD, BlueOak-1.0.0). The
JetBrains Mono font is OFL-1.1.

Known exceptions, accepted after the October 2026 dependency audit:

- The embedded web bundle includes every Shiki grammar and theme as lazy
  chunks. The upstream sources (per `tm-grammars`/`tm-themes` metadata) of grammars
  `ada`, `nginx`, `org`, `racket` and `ahk2` and theme `aurora-x` are GPL-3.0;
  `hcl`, `terraform` and `bird2` are MPL-2.0; about 30 grammars, mostly
  TextMate bundles such as `yaml` and `toml`, declare no detectable license.
- The docs site's build-only dependency `vocs` pulls `@codesandbox/nodebox`
  (Sustainable Use License, non-commercial). It is not in the published site or
  any release artifact.
- Build and test tooling includes MPL-2.0 (`lightningcss`), CC-BY-4.0
  (`caniuse-lite`) and Python-2.0 (`argparse`) packages; none are shipped.

Recheck with `pnpm licenses list -r` and the `LICENSE` files of `go list -deps`
modules when adding dependencies.
