#!/bin/sh
# Rolls each diffx Deployment that exists in the namespace to its pushed image digest.
# Usage: rollout.sh <namespace> <deployment>=<image>@<digest>...
set -eu
namespace=$1
shift
for pair in "$@"; do
  deployment=${pair%%=*}
  if ! kubectl get deployment "${deployment}" --namespace "${namespace}" >/dev/null 2>&1; then
    echo "Skipping ${deployment}: not provisioned in ${namespace} (deehomelab/k8s-cluster apps/diffx)" >&2
    continue
  fi
  kubectl set image "deployment/${deployment}" "${pair}" --namespace "${namespace}"
  kubectl rollout status "deployment/${deployment}" --namespace "${namespace}" --timeout=5m
done
