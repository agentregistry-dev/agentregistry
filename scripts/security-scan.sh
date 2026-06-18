#!/usr/bin/env bash
# Runs dependency and image security scans with snyk, trivy, and grype, writing
# a JSON report per scan to $SCANS_DIR and a summary to the terminal. A missing
# tool, unauthenticated snyk, or absent image warns and is skipped; exits 0.
#
# Each scanner emits JSON to a file and human output to the terminal in one run:
#   snyk  -> --json-file-output (human stdout + json file)
#   trivy -> --format json --output, then `trivy convert` to render to stdout
#   grype -> -o table -o json=<file>
#
# Toggle scanners off (set to any non-empty value):
#   DISABLE_SNYK_SECURITY_SCAN, DISABLE_TRIVY_SECURITY_SCAN, DISABLE_GRYPE_SECURITY_SCAN
#
# Inputs from the Makefile target:
#   GIT_COMMIT          short git hash, used in report filenames
#   SCANS_DIR           output directory for reports (default: scans)
#   SERVER_IMAGE        server image ref to scan (optional)
#   AGENTGATEWAY_IMAGE  agentgateway image ref to scan (optional)

set -o pipefail

SCANS_DIR="${SCANS_DIR:-scans}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
TS="$(date -u '+%Y%m%dT%H%M%SZ')"

# Image refs to scan. Empty values are simply skipped.
SERVER_IMAGE="${SERVER_IMAGE:-}"
AGENTGATEWAY_IMAGE="${AGENTGATEWAY_IMAGE:-}"

mkdir -p "${SCANS_DIR}"

# Scratch dir for transient artifacts (snyk image archives). Removed on any
# exit — normal, error, or interrupt (Ctrl-C / TERM) — so multi-hundred-MB
# 'docker save' tarballs can never accumulate in the temp dir.
WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

# Track which report files we produced for a final summary.
REPORTS=()

# ── helpers ──────────────────────────────────────────────────────────────────

have() { command -v "$1" >/dev/null 2>&1; }

hr() { printf '─%.0s' {1..78}; echo; }

section() {
  echo
  hr
  echo "▶ $*"
  hr
}

warn() { echo "⚠️  $*" >&2; }

report_path() {
  # report_path <scanner> <target>
  echo "${SCANS_DIR}/${1}_${2}_${GIT_COMMIT}_${TS}.json"
}

record() { REPORTS+=("$1"); }

# snyk requires authentication; treat installed-but-unauthed as skip.
snyk_authed() {
  [ -n "${SNYK_TOKEN:-}" ] && return 0
  local api
  api="$(snyk config get api 2>/dev/null)"
  [ -n "${api}" ]
}

image_present() {
  docker image inspect "$1" >/dev/null 2>&1
}

# ── snyk ─────────────────────────────────────────────────────────────────────

run_snyk() {
  if [ -n "${DISABLE_SNYK_SECURITY_SCAN:-}" ]; then
    echo "snyk: disabled via DISABLE_SNYK_SECURITY_SCAN; skipping"
    return 0
  fi
  if ! have snyk; then
    warn "snyk not installed; skipping snyk scans (see https://docs.snyk.io/snyk-cli/install-or-update-the-snyk-cli)"
    return 0
  fi
  if ! snyk_authed; then
    warn "snyk is installed but not authenticated; run 'snyk auth' or set SNYK_TOKEN. Skipping snyk scans"
    return 0
  fi

  section "snyk: dependency scan (source tree: go.mod + ui lockfiles)"
  local deps_report; deps_report="$(report_path snyk deps)"
  # --json-file-output prints human-readable output to stdout AND writes JSON.
  snyk test --all-projects --json-file-output="${deps_report}" || true
  [ -f "${deps_report}" ] && record "${deps_report}"

  scan_snyk_image "${SERVER_IMAGE}" "image-server"
  scan_snyk_image "${AGENTGATEWAY_IMAGE}" "image-agentgateway"
}

scan_snyk_image() {
  local image="$1" target="$2"
  [ -z "${image}" ] && return 0
  if ! image_present "${image}"; then
    warn "snyk: image '${image}' not found locally; skipping ${target} (build it with 'make docker docker-tag-as-dev')"
    return 0
  fi
  section "snyk: container scan (${image})"
  local rep; rep="$(report_path snyk "${target}")"
  # Scan via a local docker-archive rather than the image ref: refs with a
  # registry host (e.g. localhost:5001/...) make snyk attempt a registry pull,
  # which fails against the plain-HTTP local registry. The archive reads the
  # image already in the docker daemon and never touches the network.
  local archive="${WORKDIR}/snyk-${target}.tar"
  if docker save "${image}" -o "${archive}" 2>/dev/null; then
    snyk container test "docker-archive:${archive}" --json-file-output="${rep}" || true
  else
    warn "snyk: failed to export '${image}' via 'docker save'; skipping ${target}"
  fi
  # Free space immediately between images; the WORKDIR trap is the backstop.
  rm -f "${archive}"
  [ -f "${rep}" ] && record "${rep}"
}

# ── trivy ────────────────────────────────────────────────────────────────────

run_trivy() {
  if [ -n "${DISABLE_TRIVY_SECURITY_SCAN:-}" ]; then
    echo "trivy: disabled via DISABLE_TRIVY_SECURITY_SCAN; skipping"
    return 0
  fi
  if ! have trivy; then
    warn "trivy not installed; skipping trivy scans (see https://trivy.dev/latest/getting-started/installation/)"
    return 0
  fi

  section "trivy: dependency scan (source tree: go.mod + ui lockfiles)"
  local deps_report; deps_report="$(report_path trivy deps)"
  trivy fs --scanners vuln --format json --output "${deps_report}" . || true
  if [ -f "${deps_report}" ]; then
    record "${deps_report}"
    # Render the JSON report to the terminal without rescanning.
    trivy convert --format table "${deps_report}" || true
  fi

  scan_trivy_image "${SERVER_IMAGE}" "image-server"
  scan_trivy_image "${AGENTGATEWAY_IMAGE}" "image-agentgateway"
}

scan_trivy_image() {
  local image="$1" target="$2"
  [ -z "${image}" ] && return 0
  if ! image_present "${image}"; then
    warn "trivy: image '${image}' not found locally; skipping ${target} (build it with 'make docker docker-tag-as-dev')"
    return 0
  fi
  section "trivy: container scan (${image})"
  local rep; rep="$(report_path trivy "${target}")"
  trivy image --format json --output "${rep}" "${image}" || true
  if [ -f "${rep}" ]; then
    record "${rep}"
    trivy convert --format table "${rep}" || true
  fi
}

# ── grype ────────────────────────────────────────────────────────────────────

run_grype() {
  if [ -n "${DISABLE_GRYPE_SECURITY_SCAN:-}" ]; then
    echo "grype: disabled via DISABLE_GRYPE_SECURITY_SCAN; skipping"
    return 0
  fi
  if ! have grype; then
    warn "grype not installed; skipping grype scans (see https://github.com/anchore/grype#installation)"
    return 0
  fi

  section "grype: dependency scan (source tree: go.mod + ui lockfiles)"
  local deps_report; deps_report="$(report_path grype deps)"
  # -o table streams to stdout; -o json=<file> writes the typed report.
  grype dir:. -o table -o "json=${deps_report}" || true
  [ -f "${deps_report}" ] && record "${deps_report}"

  scan_grype_image "${SERVER_IMAGE}" "image-server"
  scan_grype_image "${AGENTGATEWAY_IMAGE}" "image-agentgateway"
}

scan_grype_image() {
  local image="$1" target="$2"
  [ -z "${image}" ] && return 0
  if ! image_present "${image}"; then
    warn "grype: image '${image}' not found locally; skipping ${target} (build it with 'make docker docker-tag-as-dev')"
    return 0
  fi
  section "grype: container scan (${image})"
  local rep; rep="$(report_path grype "${target}")"
  grype "${image}" -o table -o "json=${rep}" || true
  [ -f "${rep}" ] && record "${rep}"
}

# ── main ─────────────────────────────────────────────────────────────────────

echo "Security scan (report-only) — commit ${GIT_COMMIT}, ${TS}"
echo "Reports will be written to ${SCANS_DIR}/"

run_snyk
run_trivy
run_grype

section "Summary"
if [ "${#REPORTS[@]}" -eq 0 ]; then
  echo "No reports were produced (all scanners disabled, missing, or skipped)."
else
  echo "Reports written:"
  for r in "${REPORTS[@]}"; do
    echo "  • ${r}"
  done
fi
exit 0
