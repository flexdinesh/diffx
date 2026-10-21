# diffx

Review local Git changes in your browser without modifying your working tree or
index.

## Install

With Homebrew:

```sh
brew install flexdinesh/tap/diffx
```

Or with Go 1.25 or later:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@latest
```

See [Getting started](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/getting-started.md) for pinned and
`edge` versions.

## Quick start

From your Git repository, run:

```sh
diffx
```

Your browser opens the captured changes. Review a fixed patch instead with
`git diff | diffx`. Use `diffx --help` for usage.

## Documentation

User documentation lives in [`docs-website/`](https://github.com/flexdinesh/diffx/blob/main/docs-website/README.md):

- [Getting started](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/getting-started.md)
- [Usage](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/usage.md)
- [Reviewing changes](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/reviewing-changes.md)
- [Piped diffs](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/piped-diffs.md)
- [Agent hooks and remote ingestion](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/agent-hooks.md)
- [Agent plugins](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/plugins.md)
- [MCP](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/mcp.md)
- [WebMCP](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/webmcp.md)
- [API and data](https://github.com/flexdinesh/diffx/blob/main/docs-website/src/pages/api.md)

For development and architecture, see [docs/development.md](docs/development.md)
and [docs/architecture.md](docs/architecture.md). Publishing instructions:
[docs/release.md](docs/release.md).
