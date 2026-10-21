# Docs website

Public user documentation built with [Vocs](https://vocs.dev). Canonical source
for user-facing usage; the root README links here. Existing `docs/` remains code
and contributor documentation.

## Run locally

From the repository root, with mise installed and the project trusted:

```sh
pnpm install --frozen-lockfile
mise run docs:dev
```

Open <http://localhost:5173/>. Changes reload automatically; stop with `Ctrl+C`.
The site pins React 19.3 directly, as Vocs requires; the web app keeps the
catalog React version.

## Build

```sh
mise run docs:build
```

Static HTML and assets go to `docs-website/dist/public/` (gitignored). Dead
links fail the build. `mise run docs:check` runs the same validation. Preview a
build with `pnpm --filter @diffx/docs preview`.

The pnpm workspace also exposes `pnpm --filter @diffx/docs dev`, `build`, and
`preview`. Application builds remain independent.

## Edit

- Pages: Markdown files in `src/pages/`. Keep them plain Markdown so GitHub
  renders the files the README links to.
- Link pages with relative `page.md` paths; Vocs rewrites them to routes.
- Sidebar and site configuration: `vocs.config.ts`.
- Screenshots and other assets: add files under `public/`, then reference
  them with paths such as `/images/review.png`.

Keep pages brief; no custom layout or deployment setup yet.
