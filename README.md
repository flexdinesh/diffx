# diffx

Review local Git changes in your browser without touching your working tree or
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

See [install options](docs-website/src/pages/getting-started.md) for pinned versions
and a custom binary directory. For a developer checkout, use `mise run install`.
Go source installs are unsupported because source refs lack embedded web assets.

## Use

```sh
diffx                              # review current checkout
diffx /path/to/repo                # review another checkout
git diff | diffx                   # review a patch
diffx connect ssh://devbox         # review diffx on a remote machine
```

## Docs

- [CLI](docs/cli.md)
- [User docs](https://github.com/flexdinesh/diffx/blob/main/docs-website/README.md)
- [Development](docs/development.md), [architecture](docs/architecture.md), [releases](docs/release.md)
