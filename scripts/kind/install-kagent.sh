#!/usr/bin/env bash

set -o errexit
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${SCRIPT_DIR}/../../bin"
KAGENT="${BIN_DIR}/kagent"
KUBE_CONTEXT="${KUBE_CONTEXT:-kind-agentregistry}"

# Pinned release version — set via KAGENT_VERSION in the Makefile.
KAGENT_VERSION="${KAGENT_VERSION:?KAGENT_VERSION must be set (defined in Makefile)}"

# Download kagent CLI into the project bin directory if not already present.
if [ ! -x "${KAGENT}" ]; then
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "${ARCH}" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)
      echo "ERROR: unsupported architecture: ${ARCH}"
      exit 1
      ;;
  esac

  BINARY="kagent-${OS}-${ARCH}"
  URL="https://github.com/kagent-dev/kagent/releases/download/${KAGENT_VERSION}/${BINARY}"
  CHECKSUM_URL="${URL}.sha256"

  echo "Downloading kagent ${KAGENT_VERSION} (${BINARY}) to ${KAGENT}..."
  mkdir -p "${BIN_DIR}"
  curl -fsSL "${URL}" -o "${KAGENT}"
  chmod +x "${KAGENT}"

  # Verify checksum
  EXPECTED=$(curl -fsSL "${CHECKSUM_URL}" | awk '{print $1}')
  ACTUAL=$(sha256sum "${KAGENT}" | awk '{print $1}')
  if [ "${EXPECTED}" != "${ACTUAL}" ]; then
    echo "ERROR: checksum mismatch for ${BINARY}"
    echo "  expected: ${EXPECTED}"
    echo "  actual:   ${ACTUAL}"
    rm -f "${KAGENT}"
    exit 1
  fi
  echo "Checksum verified."
fi

# Use placeholder API keys if not set — kagent requires them at install time
# but real inference is not needed for local/CI cluster setup.
export OPENAI_API_KEY="${OPENAI_API_KEY:-fake-key-for-setup}"
export GOOGLE_API_KEY="${GOOGLE_API_KEY:-fake-key-for-setup}"

echo "Installing kagent on cluster context '${KUBE_CONTEXT}'..."
kubectl config use-context "${KUBE_CONTEXT}"
"${KAGENT}" install \
  --namespace kagent \
  --profile minimal

echo "Waiting for kagent deployments to be ready..."
kubectl --context "${KUBE_CONTEXT}" wait --for=condition=available \
  --timeout=300s deployment \
  -l app.kubernetes.io/name=kagent \
  --namespace kagent || echo "Warning: kagent not fully ready"
