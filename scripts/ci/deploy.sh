#!/bin/sh
# Builds the server and fetcher images, pushes commit and channel tags, and rolls
# the target namespace to the pushed digests. A release tag targets diffx-prod
# with the floating "release" tag; main targets diffx-staging with "main".
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
for deployment in diffx-server diffx-fetcher; do
  containers=$(kubectl get deployment "${deployment}" --namespace "${namespace}" -o jsonpath='{.spec.template.spec.containers[*].name}')
  test "${containers}" = "${deployment}" || {
    echo "Provision ${deployment} in ${namespace} from deehomelab/k8s-cluster apps/diffx before rollout" >&2
    exit 1
  }
done
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
kubectl set image deployment/diffx-server "diffx-server=${server_image}" --namespace "${namespace}"
kubectl set image deployment/diffx-fetcher "diffx-fetcher=${fetcher_image}" --namespace "${namespace}"
kubectl rollout status deployment/diffx-server --namespace "${namespace}" --timeout=5m
kubectl rollout status deployment/diffx-fetcher --namespace "${namespace}" --timeout=5m
