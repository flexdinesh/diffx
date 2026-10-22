---
title: Getting started
description: Install diffx and open your first local review.
---

# Getting started

## Install

With Homebrew:

```sh
brew install flexdinesh/tap/diffx
```

Latest stable release with Go 1.27.3 or later:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@latest
```

Or install a specific stable version:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@v0.2.0
```

Or install the latest `main` changes that passed CI:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@edge
```

## Open a review

From your Git repository, run:

```sh
diffx
```

Your browser opens the captured changes. Use the file list to navigate, switch
between all, staged, and unstaged changes, and add comments as you review.

To review another repository:

```sh
diffx /path/to/repo
```

## Capture new changes

A review is a fixed snapshot. After editing your code, use **Sync all
checkouts** (Alt+R) to capture the latest changes. Comments and review progress
persist across restarts.

See [Usage](usage.md) for all options.
