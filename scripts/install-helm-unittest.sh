#!/usr/bin/env bash
# Installs the helm-unittest plugin if it is not already present.
# Attempts installation with HELM_PLUGIN_INSTALL_FLAGS first, then retries
# without flags as a fallback (see https://github.com/helm/helm/issues/31490).

set -o errexit
set -o pipefail

HELM="${HELM:-helm}"
HELM_PLUGIN_UNITTEST_URL="${HELM_PLUGIN_UNITTEST_URL:-https://github.com/helm-unittest/helm-unittest}"
HELM_PLUGIN_UNITTEST_VERSION="${HELM_PLUGIN_UNITTEST_VERSION:-v1.0.3}"
HELM_PLUGIN_INSTALL_FLAGS="${HELM_PLUGIN_INSTALL_FLAGS:---verify=false}"

echo "Checking for helm-unittest plugin..."

if "${HELM}" plugin list | awk '{print $1}' | grep -q '^unittest$'; then
  echo "helm-unittest plugin already installed"
  exit 0
fi

echo "helm-unittest plugin not found — installing from ${HELM_PLUGIN_UNITTEST_URL}"

if "${HELM}" plugin install "${HELM_PLUGIN_UNITTEST_URL}" \
    --version "${HELM_PLUGIN_UNITTEST_VERSION}" \
    ${HELM_PLUGIN_INSTALL_FLAGS}; then
  echo "helm-unittest installed (with HELM_PLUGIN_INSTALL_FLAGS)"
  exit 0
fi

# Retry without flags: some Helm versions fail with extra plugin install flags
# due to https://github.com/helm/helm/issues/31490
echo "Install with HELM_PLUGIN_INSTALL_FLAGS failed; retrying without flags..."
if "${HELM}" plugin install "${HELM_PLUGIN_UNITTEST_URL}" \
    --version "${HELM_PLUGIN_UNITTEST_VERSION}"; then
  echo "helm-unittest installed (without HELM_PLUGIN_INSTALL_FLAGS)"
  exit 0
fi

echo "ERROR: helm-unittest install failed. Check network / plugin URL."
exit 1
