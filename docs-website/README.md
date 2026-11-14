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
  renders the files the README links to. `index.mdx` is the exception: it holds
  the home page hero.
- Link pages with relative `page.md` paths; Vocs rewrites them to routes.
- Sidebar, top nav and site configuration: `vocs.config.ts`.
- Screenshots and other assets: add files under `public/`, then reference
  them with paths such as `/images/review.png`.

Keep pages brief.

## Deploy (Cloudflare Workers)

The site deploys as static files through Cloudflare Workers Static Assets and
Workers Builds, with no Worker code. `wrangler.jsonc` owns the Worker names,
compatibility date, assets directory (`dist/public`) and 404 handling
(`404-page` serves `404.html`). Wrangler stays out of the lockfile; deploys
run it through `npx`. Build and deploy commands live in the dashboard:

| Setting         | Production                                                                  | Staging                                                                  |
| --------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------ |
| Worker name     | `diffx-docs`                                                                | `diffx-docs-staging`                                                     |
| Root directory  | repository root                                                             | repository root                                                          |
| Build command   | `pnpm install --frozen-lockfile --filter @diffx/docs... && pnpm build:docs` | same                                                                     |
| Deploy command  | `npx wrangler deploy --config docs-website/wrangler.jsonc --env ""`         | `npx wrangler deploy --config docs-website/wrangler.jsonc --env staging` |
| Build variables | `SKIP_DEPENDENCY_INSTALL=1`, `HUSKY=0`                                      | same, plus `DIFFX_DOCS_URL=https://stagingdocs.diffx.app`                |

Worker names must match `wrangler.jsonc`. Node comes from `.node-version` and
pnpm from `packageManager`. The filtered install skips the application
workspaces; `HUSKY=0` skips the root `prepare` hook.

`DIFFX_DOCS_URL` is read at build time and sets the site URL. Production leaves
it unset, which defaults to `https://docs.diffx.app`. Any non-production URL
emits `noindex, nofollow`, points canonical links at production, and skips
`sitemap.xml` and `robots.txt`.
