---
title: Getting started
description: Install diffx and open your first local review.
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

## Open a review

From your Git repository, run:

```sh
diffx
```

Your browser opens the captured changes. Use the file list to navigate, switch
between Uncommitted, Branch changes, and Combined, and add comments as you review.
Uncommitted also offers All, Staged, and Unstaged views. Available comparisons
depend on what the snapshot captured.

To review another repository:

```sh
diffx /path/to/repo
```

## Capture new changes

A review is a fixed snapshot. After editing your code, use **Sync all
checkouts** (Alt+R) to capture the latest changes. Comments and review progress
persist across restarts.

See [Usage](usage.md) for all options.
