# Releases

## Stable releases

Only the manual **Release** workflow creates stable version tags and GitHub
Releases. It checks out the latest `main` when the run starts and marks the
release as GitHub's latest stable release. Dispatches from other branches are
skipped.

Full test suites run locally through the pre-push hook; CI never runs it.
Workflows set `HUSKY=0`, and the `release:edge` and `release:tag` pushes pass
`--no-verify`. Release verification runs static checks, rebuilds generated
files, rejects drift, and smoke-tests the distribution before publishing, using
the same `mise run check:ci` task as PR CI.
Release steps run through `release:*` mise tasks and pnpm scripts; GoReleaser
is installed only by `mise run release:publish`.

Install the latest stable release or a specific version:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@latest
go install github.com/flexdinesh/diffx/cmd/diffx@v0.2.0
```

Required repository secret:

- `HOMEBREW_TAP_TOKEN`: fine-grained token with contents write and pull request
  write access to `flexdinesh/homebrew-tap`.

1. Merge release-ready code to `main`.
2. Run the **Release** workflow with `main` selected. It requires no inputs.
3. The workflow verifies the repository, commits the embedded web assets on top
   of the `main` tip as `release: vX.Y.Z`, pushes only the tag, and publishes
   the GitHub Release with GoReleaser from that release commit.
4. It generates `Formula/diffx.rb` and opens or updates a pull request in
   `flexdinesh/homebrew-tap`.
5. Merge the tap pull request after its Homebrew checks pass.

Each release contains checksums plus Linux and macOS archives for amd64 and
arm64. Windows archives are also published. Archives include the native binary,
README, and license. The embedded web application needs no installed Node.js
runtime.

Stop running binaries before upgrading. Schema 11 and 12 state upgrades in place to
schema 13 and survives subsequent starts; older, future or unrecognized schemas are refused without
resetting data. Local `diffx` offers to back up older state beside it and start
fresh, or does so with `--reset-state`. For servers, back up the state directory
and explicitly choose a fresh database path if discarding incompatible state. Upgrade server and collectors
together for ingestion protocol 4, deploying the server first: new producers send
optional activity metadata that older servers reject. Existing protocol 4
submissions without that metadata still replay unchanged and need no reset. In-memory state lasts only for one process.

The tap branch is deterministic per version, such as `diffx-v0.2.0`.
Rerunning a release whose tag's first parent is still current `main` reuses the existing
GitHub artifacts and updates the same tap pull request. Published artifacts are
not rebuilt or replaced.

The tap repository owns Homebrew style, strict audit, install, and formula test
checks before merge.

## Development installs

Install the latest `main` commit that passed CI, including unreleased changes:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@edge
```

After the `checks` job passes on a push to `main`, CI's `edge` job builds the web
assets, commits them on top of that `main` commit, and force-pushes the result to
the `edge` branch with a lease. Runs are serialized and superseded runs are
cancelled; a run whose commit `edge` already includes is skipped. When the latest
release commit is not an ancestor, it becomes a second parent so Go derives
`edge` pseudo-versions that sort after that release. Stable `@latest` installs
remain on the highest stable version tag. Module proxies may briefly cache branch
lookups; use `GOPROXY=direct` when an immediate refresh is needed.

`@main`, `@<sha>` and feature branches do not build: they lack the embedded
assets. Tags created before this policy point at `main` commits that still
contain them and remain installable.

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

`go install` builds module source exactly as it is at the ref and runs no
frontend build, so the binary embeds `internal/webui/dist` from that ref. The
directory is ignored on `main`, which avoids recommitting the bundle on every UI
change. CI commits it only on installable refs, each one commit on top of `main`
created with `tools/asset-commit.sh`:

| Ref      | Trigger                              | Commit                                        |
| -------- | ------------------------------------ | --------------------------------------------- |
| `vX.Y.Z` | Manual **Release** workflow          | `release: vX.Y.Z`: `main` tip plus assets     |
| `edge`   | Push to `main` after `checks` passes | `edge: <sha>`: that `main` commit plus assets |

Release archives are built from the release commit, so archives and source
installs share commit identity. Version selection and reruns compare a tag's
first parent (`<tag>^1`) with the `main` tip. The CI identity must be allowed to
push the `edge` branch and version tags.

Stage assets locally before Go builds and tests:

```sh
mise run web:stage
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
