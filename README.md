# diffx

Review local Git changes in your browser without touching your working tree or
index.

## Install

```sh
brew install flexdinesh/tap/diffx                         # Homebrew
go install github.com/flexdinesh/diffx/cmd/diffx@latest   # Go 1.27.3+, stable
go install github.com/flexdinesh/diffx/cmd/diffx@edge     # latest main
```

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
