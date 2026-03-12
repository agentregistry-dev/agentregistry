#!/usr/bin/env bash

set -o errexit
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Use kind via go tool directives (go.mod) so no separate install step is needed.
KIND=(go tool kind)

KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-agentregistry}
KIND_IMAGE_VERSION=${KIND_IMAGE_VERSION:-1.34.0}
KIND_CLUSTER_CONTEXT=kind-${KIND_CLUSTER_NAME}

# Optional: set REG_NAME and REG_PORT to configure the cluster to use a local
# registry. If unset, registry-specific steps (containerd config, network
# connect, ConfigMap) are skipped.
REG_NAME=${REG_NAME:-}
REG_PORT=${REG_PORT:-}

# 1. Create kind cluster with containerd registry config dir enabled
#
# NOTE: the containerd config patch is not necessary with images from kind v0.27.0+
# It may enable some older images to work similarly.
# If you're only supporting newer releases, you can just use `kind create cluster` here.
#
# See:
# https://github.com/kubernetes-sigs/kind/issues/2875
# https://github.com/containerd/containerd/blob/main/docs/cri/config.md#registry-configuration
# See: https://github.com/containerd/containerd/blob/main/docs/hosts.md
# On Linux, Docker containers cannot reach the host via 127.0.0.1 (loopback is
# network-namespace local). Bind the API server on all interfaces so it is
# reachable from the Docker bridge network, and patch the kubeconfig afterwards
# to use the bridge gateway IP instead of 0.0.0.0.
if [ "$(uname -s)" = "Linux" ]; then
  echo "Linux: patching Kind config to bind API server on 0.0.0.0..."
  sed -i 's/apiServerAddress: "127.0.0.1"/apiServerAddress: "0.0.0.0"/' \
    "${SCRIPT_DIR}/kind-config.yaml"
fi

"${KIND[@]}" create cluster --name "${KIND_CLUSTER_NAME}" \
  --config "${SCRIPT_DIR}/kind-config.yaml" \
  --image="kindest/node:v${KIND_IMAGE_VERSION}"

if [ "$(uname -s)" = "Linux" ]; then
  GATEWAY=$(docker network inspect bridge -f '{{range .IPAM.Config}}{{.Gateway}}{{end}}')
  if [ -n "${GATEWAY}" ]; then
    echo "Linux: patching kubeconfig to use Docker bridge gateway ${GATEWAY}..."
    sed -i "s|server: https://0.0.0.0:|server: https://${GATEWAY}:|g" \
      "${HOME}/.kube/config"
  fi
fi

if [ -z "${REG_NAME}" ] || [ -z "${REG_PORT}" ]; then
  echo "REG_NAME/REG_PORT not set — skipping local registry configuration"
  exit 0
fi

# 2. Add the registry config to the nodes
#
# This is necessary because localhost resolves to loopback addresses that are
# network-namespace local.
# In other words: localhost in the container is not localhost on the host.
#
# We want a consistent name that works from both ends, so we tell containerd to
# alias localhost:${REG_PORT} to the registry container when pulling images
REGISTRY_DIR="/etc/containerd/certs.d/localhost:${REG_PORT}"
for node in $("${KIND[@]}" get nodes --name "${KIND_CLUSTER_NAME}"); do
  docker exec "${node}" mkdir -p "${REGISTRY_DIR}"
  cat <<EOF | docker exec -i "${node}" cp /dev/stdin "${REGISTRY_DIR}/hosts.toml"
[host."http://${REG_NAME}:5000"]
EOF
done

# 3. Connect the registry to the cluster network if not already connected
# This allows kind to bootstrap the network but ensures they're on the same network
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REG_NAME}")" = 'null' ]; then
  docker network connect "kind" "${REG_NAME}"
fi

# 4. Document the local registry
# https://github.com/kubernetes/enhancements/tree/master/keps/sig-cluster-lifecycle/generic/1755-communicating-a-local-registry
cat <<EOF | kubectl --context ${KIND_CLUSTER_CONTEXT} apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REG_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF
