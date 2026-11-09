# Isolated check image. check-runner builds one target per group from named
# contexts it materializes with the same predicate as group fingerprints (files
# live under each context's tree/ so the repository .dockerignore stays inert):
# manifests, core, extra (contracts-only files), site-source, site-manifests,
# mise.
# Stages are ordered from least to most volatile so every worktree and run
# shares toolchain, dependency and (for identical sources) build layers.
FROM debian:bookworm-slim@sha256:99c0b25c46d0214d5a2091092e7908907dcaecd6a962cde0ac700acee222b6f3 AS base
ENV DEBIAN_FRONTEND=noninteractive
# Desktop runtime and window-acceptance packages live in this shared layer so
# desktop groups reuse the same application build as every other group.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl git bash build-essential ruby unzip xz-utils \
    libwebkit2gtk-4.1-0 libgtk-3-0 libpango-1.0-0 libcairo2 libgdk-pixbuf-2.0-0 \
    fontconfig fonts-dejavu-core fonts-noto-color-emoji \
    xvfb xauth dbus dbus-x11 libx11-6 libxtst6 xclip xdotool scrot \
  && rm -rf /var/lib/apt/lists/*
COPY --from=mise mise /usr/local/bin/mise
ENV MISE_YES=1 HUSKY=0 GOTOOLCHAIN=local GOMAXPROCS=2 GOFLAGS=-p=2 \
    pnpm_config_workspace_concurrency=1 pnpm_config_store_dir=/root/.cache/pnpm-store \
    MYGO_GPU=0
WORKDIR /workspace

FROM base AS toolchain
COPY --from=manifests tree/mise.toml tree/mise.lock tree/.node-version tree/package.json /workspace/
RUN mise trust --all && mise install --locked

FROM toolchain AS deps
COPY --from=manifests tree/ /workspace/
RUN --mount=type=cache,id=diffx-check-pnpm-store,target=/root/.cache/pnpm-store,sharing=locked \
    mise exec -- pnpm install --frozen-lockfile
RUN mise exec -- go mod download && mise exec -- go -C apps/desktop mod download
RUN mise exec -- pnpm --filter @diffx/web exec playwright install --with-deps chromium

# The baseline commit precedes builds so generated-drift checks see changes
# made by builds and generators.
FROM deps AS source
COPY --from=core tree/ /workspace/
RUN git init -q \
  && git add --all --force . ':!node_modules' ':!**/node_modules' \
  && GIT_AUTHOR_NAME=Validation GIT_AUTHOR_EMAIL=validation@localhost \
     GIT_COMMITTER_NAME=Validation GIT_COMMITTER_EMAIL=validation@localhost \
     GIT_AUTHOR_DATE=2000-01-01T00:00:00Z GIT_COMMITTER_DATE=2000-01-01T00:00:00Z \
     git -c core.hooksPath=/dev/null commit -qm baseline

FROM source AS built
RUN --mount=type=cache,id=diffx-check-gocache,target=/root/.cache/go-build \
    mise run --force build && mise run --force plugins:build

# Contracts-only files join the baseline without rebuilding or committing
# build outputs.
FROM built AS built-docs
RUN --mount=type=bind,from=extra,source=tree,target=/extra \
  cp -a /extra/. /workspace/ \
  && git -C /extra --git-dir=/workspace/.git --work-tree=/extra add --force --ignore-removal . \
  && GIT_AUTHOR_NAME=Validation GIT_AUTHOR_EMAIL=validation@localhost \
     GIT_COMMITTER_NAME=Validation GIT_COMMITTER_EMAIL=validation@localhost \
     GIT_AUTHOR_DATE=2000-01-01T00:00:00Z GIT_COMMITTER_DATE=2000-01-01T00:00:00Z \
     git -c core.hooksPath=/dev/null commit -q --amend --no-edit

FROM built AS desktop
RUN --mount=type=cache,id=diffx-check-gocache,target=/root/.cache/go-build \
    mise run --force desktop:stage

FROM built-docs AS desktop-docs
RUN --mount=type=cache,id=diffx-check-gocache,target=/root/.cache/go-build \
    mise run --force desktop:stage

FROM toolchain AS site-deps
COPY --from=site-manifests tree/ /workspace/
RUN --mount=type=cache,id=diffx-check-pnpm-store,target=/root/.cache/pnpm-store,sharing=locked \
    mise exec -- pnpm install --frozen-lockfile
RUN mise exec -- go mod download

FROM site-deps AS site
COPY --from=site-source tree/ /workspace/
RUN git init -q \
  && git add --all --force . ':!node_modules' ':!**/node_modules' \
  && GIT_AUTHOR_NAME=Validation GIT_AUTHOR_EMAIL=validation@localhost \
     GIT_COMMITTER_NAME=Validation GIT_COMMITTER_EMAIL=validation@localhost \
     GIT_AUTHOR_DATE=2000-01-01T00:00:00Z GIT_COMMITTER_DATE=2000-01-01T00:00:00Z \
     git -c core.hooksPath=/dev/null commit -qm baseline
