# Releases

## Stable releases

Only the manual **Release** workflow creates stable version tags and GitHub
Releases. It checks out the latest `main` when the run starts and marks the
release as GitHub's latest stable release. Dispatches from other branches are
skipped. Releases publish downloadable binaries; they never deploy servers (see
[Homelab container images](#homelab-container-images)).

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
`diffx_edge_<os>_<arch>.tar.gz` plus `checksums.txt`. Edge is excluded from
GitHub's latest stable release and from `install.sh`'s default selection.
The legacy `edge` branch is no longer updated.

Downloads can fail or encounter a checksum mismatch while rolling assets are
being replaced. Retry after publication finishes; the installer preserves any
existing binary on failure. Use `--bin-dir DIR` to override `~/.local/bin`.

Go source installs (`@latest`, `@edge`, `@<sha>` and branch refs) are unsupported
because source refs lack embedded assets. Historical binary releases remain
installable separately with `install.sh --version vX.Y.Z`. For development from
a configured checkout, use `mise run install`.

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

## Homelab container images

Woodpecker publishes `diffx-server`, `diffx-fetcher` and `diffx-console` images
to `registry.local.deebox.xyz/home` for the Kubernetes app in
`deehomelab/k8s-cluster` (`apps/diffx`). Server deploys are independent of CLI
releases and version tags; prod runs images built once for staging:

- `.woodpecker/deploy.yml`: pushes to `main` stage the embedded web assets,
  build all Dockerfile targets, tag `:<commit>` and `:main`, and roll
  `diffx-staging` to the pushed digests. A manual Woodpecker run on `main` does
  the same without a push.
- `.woodpecker/promote.yml`: deploying a successful `main` pipeline to the
  `prod` environment (Woodpecker pipeline **Deploy**, or
  `woodpecker-cli pipeline deploy <repo> <pipeline> prod`) pulls that commit's
  `:<commit>` images, retags them `:release` (the tag the prod overlay pins) and
  rolls `diffx-prod` to the same digests staging ran, without rebuilding.
  Promotion fails when the commit has no staging images. Promoting an earlier
  commit rolls back only when its server accepts the migrated database schema.
  The repository must allow deployments in Woodpecker.

Only Deployments that exist in the namespace roll; prod has no fetcher until its
GitHub integration is enabled. Images are published even before the k8s-cluster
manifests are provisioned. Woodpecker must trust the repository for the Docker
socket and hold a `KUBECONFIG` secret with deployment access to both namespaces.
Pushes to `main` deploy staging without waiting for GitHub Actions; verification
is performed through explicit local PR validation. Staging and prod must drop
`DIFFX_SERVER_SIGNUP` and `DIFFX_SERVER_PASSWORD_SIGNIN` before deploying a
build without them, since the server refuses to start while either is set; set
admission in the console's Settings page after deploy.
