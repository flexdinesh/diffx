---
title: Getting started
description: Install the diffx CLI and send your first review to a diffx server.
---

# Getting started

## Install

Install the latest stable CLI:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh
```

Or install a specific stable version:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh -s -- --version v0.2.0
```

Supports macOS and Linux on amd64 and arm64. No Go or Node.js required.
Requires `curl`, `tar`, and either `sha256sum` or `shasum`.
The installer defaults to `~/.local/bin`, verifies SHA-256 before replacing the
binary, and preserves an existing binary on failure. It uses no sudo and leaves
shell profiles unchanged. Ensure the install directory is on your `PATH`.

Choose a writable binary directory:

```sh
curl -fsSL https://raw.githubusercontent.com/flexdinesh/diffx/main/install.sh | sh -s -- --bin-dir "$HOME/bin"
```

The installer installs only `diffx`, not the remote server or agent plugins.
Go source installs (`@latest` or a commit SHA) are unsupported because source
refs lack embedded web assets. Developers can use `mise run install` from a
configured checkout.

## Connect to a server

Sign in to [diffx.app](https://diffx.app) or your own
[diffx server](agent-hooks.md). In the top-right account settings, create a
`collector` API token; its secret shows only once. Point the CLI at the server:

```sh
diffx config set remote.url https://diffx.app
diffx config set remote.token - < /path/to/private-token
diffx remote status
```

## Send a review

From your Git repository, run:

```sh
diffx sync
```

The checkout's changes arrive on the server's **Reviews** bench. Open the
review to navigate files, switch between Uncommitted, Branch changes and
Combined, mark files reviewed and add comments. Uncommitted also offers All,
Staged and Unstaged views. Available comparisons depend on what the snapshot
captured.

## Capture new changes

A review is a fixed snapshot. After editing your code, run `diffx sync` again;
the review announces the newer snapshot. Comments re-anchor to it, and reviewed
marks stay on files whose patch is unchanged. Install an [agent plugin](plugins.md) to sync automatically after each
agent turn.

See [Usage](usage.md) for sync options and configuration.
