#!/bin/sh
# Builds the server and fetcher images, pushes commit and channel tags, then rolls
# the diffx Deployments that exist in the target namespace to the pushed digests.
# A release tag targets diffx-prod with the floating "release" tag; main targets
# diffx-staging with "main". Prod runs without the fetcher until GitHub is enabled.
set -eu
apk add --no-cache curl
sh scripts/ci/setup-kubectl.sh
if test -n "${CI_COMMIT_TAG:-}"; then
  namespace=diffx-prod
  channel=release
  version=${CI_COMMIT_TAG}
else
  namespace=diffx-staging
  channel=main
  version=${CI_COMMIT_SHA}
fi
push_digest() {
  push_output=$(docker push "${1}") || return 1
  digest=$(printf '%s\n' "${push_output}" | awk '/digest: sha256:/ { print $3 }')
  test -n "${digest}" || return 1
  printf '%s@%s' "${1%:*}" "${digest}"
}
server_tag="${REGISTRY}/diffx-server:${version}"
fetcher_tag="${REGISTRY}/diffx-fetcher:${version}"
docker build --tag "${server_tag}" .
docker build --target fetcher --tag "${fetcher_tag}" .
server_image=$(push_digest "${server_tag}")
fetcher_image=$(push_digest "${fetcher_tag}")
for tag in "${server_tag}" "${fetcher_tag}"; do
  docker tag "${tag}" "${tag%:*}:${channel}"
  docker push "${tag%:*}:${channel}"
done
echo "${version}: ${server_image} ${fetcher_image}"
for pair in "diffx-server=${server_image}" "diffx-fetcher=${fetcher_image}"; do
  deployment=${pair%%=*}
  if ! kubectl get deployment "${deployment}" --namespace "${namespace}" >/dev/null 2>&1; then
    echo "Skipping ${deployment}: not provisioned in ${namespace} (deehomelab/k8s-cluster apps/diffx)" >&2
    continue
  fi
  kubectl set image "deployment/${deployment}" "${pair}" --namespace "${namespace}"
  kubectl rollout status "deployment/${deployment}" --namespace "${namespace}" --timeout=5m
done
