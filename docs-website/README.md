# Docs website

Public user documentation built with Hugo and Hextra. Independent of the
application; existing `docs/` remains code and contributor documentation.

## Run locally

From the repository root, with mise installed and the project trusted:

```sh
mise run docs:dev
```

Open <http://localhost:1313/>. Changes reload automatically; stop with `Ctrl+C`.
Mise installs the pinned Hugo extended binary when needed. The theme is pinned
in this directory's `go.mod` and `go.sum`; its first download needs network access.
The website needs Go and Git for Hugo modules, with no Node dependencies.

## Build

```sh
mise run docs:build
```

Static HTML and assets go to `docs-website/dist/` (gitignored). Build warnings
fail the command. `mise run docs:check` runs the same validation.

The pnpm workspace also exposes `pnpm --filter @diffx/docs dev`, `build`, and
`check` through mise. Application builds remain independent.

## Edit

- Homepage: `content/_index.md`.
- Guides: Markdown files in `content/`; use `weight` to order the sidebar.
- Screenshots and other assets: add files under `static/`, then reference them
  with paths such as `/images/review.png`.
- Site configuration: `hugo.yaml`.

Use Hextra's built-in docs layout, search, code blocks, and theme toggle. Keep
pages brief; no custom layout or deployment setup yet.
