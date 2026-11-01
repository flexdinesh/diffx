#!/bin/sh
# Promotes the staging images built for CI_COMMIT_SHA to diffx-prod without rebuilding:
# retags them with the floating "release" tag the prod overlay pins, then rolls the
# provisioned prod Deployments to the same digests staging ran.
set -eu
: "${CI_COMMIT_SHA:?CI_COMMIT_SHA required}"
: "${REGISTRY:?REGISTRY required}"
apk add --no-cache curl
sh scripts/ci/setup-kubectl.sh
staging_digest() {
  docker pull "${1}:${CI_COMMIT_SHA}" >/dev/null || return 1
  digest=$(docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "${1}:${CI_COMMIT_SHA}" | grep -m 1 "^${1}@sha256:") || return 1
  printf '%s' "${digest}"
}
set --
for name in diffx-server diffx-fetcher diffx-console; do
  repository="${REGISTRY}/${name}"
  image=$(staging_digest "${repository}") || {
    echo "No staging image ${repository}:${CI_COMMIT_SHA}; promote a commit that main deployed to staging" >&2
    exit 1
  }
  docker tag "${repository}:${CI_COMMIT_SHA}" "${repository}:release"
  docker push "${repository}:release"
  set -- "$@" "${name}=${image}"
done
echo "${CI_COMMIT_SHA}: $*"
sh scripts/ci/rollout.sh diffx-prod "$@"
