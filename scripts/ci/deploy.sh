#!/bin/sh
# Builds the server, fetcher and console images for a main commit, pushes the commit and
# floating "main" tags, then rolls the diffx-staging Deployments to the pushed digests.
# scripts/ci/promote.sh later promotes these same commit images to diffx-prod.
set -eu
: "${CI_COMMIT_SHA:?CI_COMMIT_SHA required}"
apk add --no-cache curl
sh scripts/ci/setup-kubectl.sh
push_digest() {
  push_output=$(docker push "${1}") || return 1
  digest=$(printf '%s\n' "${push_output}" | awk '/digest: sha256:/ { print $3 }')
  test -n "${digest}" || return 1
  printf '%s@%s' "${1%:*}" "${digest}"
}
server_tag="${REGISTRY}/diffx-server:${CI_COMMIT_SHA}"
fetcher_tag="${REGISTRY}/diffx-fetcher:${CI_COMMIT_SHA}"
console_tag="${REGISTRY}/diffx-console:${CI_COMMIT_SHA}"
docker build --tag "${server_tag}" .
docker build --target fetcher --tag "${fetcher_tag}" .
docker build --target console --tag "${console_tag}" .
server_image=$(push_digest "${server_tag}")
fetcher_image=$(push_digest "${fetcher_tag}")
console_image=$(push_digest "${console_tag}")
for tag in "${server_tag}" "${fetcher_tag}" "${console_tag}"; do
  docker tag "${tag}" "${tag%:*}:main"
  docker push "${tag%:*}:main"
done
echo "${CI_COMMIT_SHA}: ${server_image} ${fetcher_image} ${console_image}"
sh scripts/ci/rollout.sh diffx-staging \
  "diffx-server=${server_image}" "diffx-fetcher=${fetcher_image}" "diffx-console=${console_image}"
