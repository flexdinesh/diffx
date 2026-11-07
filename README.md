<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs-website/public/logo-dark.svg">
  <img src="docs-website/public/logo-light.svg" alt="diffx" height="48">
</picture>

# diffx

Review Git changes in desktop or browser without touching your working tree or
index.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh
```

Installs the latest stable CLI into `~/.local/bin` on macOS/Linux amd64/arm64.
No Go or Node.js required. Verifies SHA-256 before replacing an existing binary;
uses no sudo and leaves shell profiles unchanged. Ensure `~/.local/bin` is on `PATH`.

For the latest `main` build:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-edge.sh | sh
```

For the experimental desktop and matching CLI pair:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install-desktop.sh | sh
```

This replaces both `diffx` and `diffx-desktop` launchers. Desktop is the default
client; a CLI-only install falls back to a foreground browser viewer. See
[desktop installation](docs/release.md#experimental-desktop-webview).

See [install options](docs-website/src/pages/getting-started.md) for pinned versions
and a custom binary directory. For a developer checkout, use `mise run install`.
Go source installs are unsupported because source refs lack embedded web assets.

## Use

```sh
diffx                              # review current checkout
diffx /path/to/repo                # review another checkout
git diff | diffx                   # review a patch
diffx --browser                    # foreground browser viewer
diffx remote ~/work                # foreground personal TLS server; prints invitation
diffx connect STRING               # pair using printed diffx1_ invitation
diffx connect ssh://devbox         # review an existing remote personal server
```

## Docs

- [CLI](docs/cli.md)
- [User docs](https://github.com/flexdinesh/diffx/blob/main/docs-website/README.md)
- [Development](docs/development.md), [architecture](docs/architecture.md), [releases](docs/release.md)
