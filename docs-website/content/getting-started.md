---
title: Getting started
description: Install diffx and open your first local review.
weight: 1
---

## Install

With Homebrew:

```sh
brew install flexdinesh/tap/diffx
```

Or with Go 1.25 or later:

```sh
go install github.com/flexdinesh/diffx/cmd/diffx@latest
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

A review is a fixed snapshot. After editing your code, stop diffx with `Ctrl+C`
and run it again to capture the latest changes. Comments and review progress
persist across restarts.
