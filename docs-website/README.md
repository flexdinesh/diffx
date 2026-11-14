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

## Deploy

Production (`https://docs.diffx.app`) deploys through Cloudflare. Staging
(`https://stagingdocs.diffx.app`, LAN only) runs in the homelab Kubernetes
cluster, deployed by the diffx staging pipeline (`k8s-cluster`, `apps/diffx`).

### Production (Cloudflare Workers)

The site deploys as static files through Cloudflare Workers Static Assets and
Workers Builds, with no Worker code. `wrangler.jsonc` owns the Worker name,
compatibility date, assets directory (`dist/public`) and 404 handling
(`404-page` serves `404.html`). Wrangler stays out of the lockfile; deploys
run it through `npx`. Build and deploy commands live in the dashboard:

| Setting         | Value                                                                       |
| --------------- | --------------------------------------------------------------------------- |
| Worker name     | `diffx-docs`                                                                |
| Root directory  | repository root                                                             |
| Build command   | `pnpm install --frozen-lockfile --filter @diffx/docs... && pnpm build:docs` |
| Deploy command  | `npx wrangler deploy --config docs-website/wrangler.jsonc`                  |
| Build variables | `SKIP_DEPENDENCY_INSTALL=1`, `HUSKY=0`                                      |

The Worker name must match `wrangler.jsonc`. Node comes from `.node-version`
and pnpm from `packageManager`. The filtered install skips the application
workspaces; `HUSKY=0` skips the root `prepare` hook.

### Staging (Kubernetes)

`Dockerfile` serves the built site with unprivileged nginx on port 8080.
`nginx.conf` mirrors the Cloudflare routing: paths resolve to
`<path>/index.html` or `<path>.html`, unknown paths return `404.html` with
status 404, and `/healthz` answers probes. The image runs with a read-only root
filesystem when `/tmp` is writable.

```sh
DIFFX_DOCS_URL=https://stagingdocs.diffx.app pnpm build:docs
docker build --file docs-website/Dockerfile docs-website
```

### Site URL

`DIFFX_DOCS_URL` is read at build time and sets the site URL. Production leaves
it unset, which defaults to `https://docs.diffx.app`. Any non-production URL
emits `noindex, nofollow`, points canonical links at production, and skips
`sitemap.xml` and `robots.txt`.
