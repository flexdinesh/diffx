# Staging docs image, served by nginx on 8080 as a non-root user. Build the site
# first, then use this directory as the context:
#   DIFFX_DOCS_URL=https://stagingdocs.diffx.app pnpm build:docs
#   docker build --file docs-website/Dockerfile docs-website
# Production deploys through Cloudflare (wrangler.jsonc) instead.
FROM nginxinc/nginx-unprivileged:1.30.6-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY dist/public /usr/share/nginx/html
