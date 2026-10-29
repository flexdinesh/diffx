#!/bin/sh
set -eu

: "${KUBECTL_VERSION:?KUBECTL_VERSION required}"
: "${KUBECONFIG_CONTENT:?KUBECONFIG_CONTENT required}"
: "${KUBECONFIG:?KUBECONFIG required}"

case "$(uname -m)" in
  x86_64) kubectl_arch=amd64 ;;
  aarch64) kubectl_arch=arm64 ;;
  *)
    echo "Unsupported architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

kubectl_url="https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${kubectl_arch}"
curl -fsSLo /tmp/kubectl "${kubectl_url}/kubectl"
curl -fsSLo /tmp/kubectl.sha256 "${kubectl_url}/kubectl.sha256"
printf '%s  %s\n' "$(cat /tmp/kubectl.sha256)" /tmp/kubectl | sha256sum -c -
install -m 0755 /tmp/kubectl /usr/local/bin/kubectl

printf '%s\n' "${KUBECONFIG_CONTENT}" >"${KUBECONFIG}"
chmod 600 "${KUBECONFIG}"
