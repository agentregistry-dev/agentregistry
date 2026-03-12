#!/usr/bin/env bash

set -o errexit
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_DIR="${SCRIPT_DIR}/../../bin"
KAGENT="${BIN_DIR}/kagent"
KUBE_CONTEXT="${KUBE_CONTEXT:-kind-agentregistry}"

# Download kagent CLI into the project bin directory if not already present
if [ ! -x "${KAGENT}" ]; then
  echo "Downloading kagent CLI to ${KAGENT}..."
  mkdir -p "${BIN_DIR}"
  # Ignore the get-kagent post-install PATH check — we invoke kagent directly
  # via ${KAGENT} so it does not need to be on $PATH.
  curl -sL https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | \
    KAGENT_INSTALL_DIR="${BIN_DIR}" bash || true
  if [ ! -x "${KAGENT}" ]; then
    echo "ERROR: kagent binary not found at ${KAGENT} after installation"
    exit 1
  fi
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
