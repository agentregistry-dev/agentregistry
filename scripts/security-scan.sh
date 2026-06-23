#!/usr/bin/env bash
# Security scanning with snyk, trivy, and grype. Two subcommands:
#
#   scan                     Run all scanners over deps + images, write a JSON
#                            report per scan plus a condensed summary into
#                            $SCANS_DIR/<hash>/, and print to the terminal.
#   compare <before> <after> Diff two runs' condensed summaries by commit hash,
#                            reporting which findings were resolved/introduced.
#
# scan: a missing tool, unauthenticated snyk, or absent image warns and is
# skipped; exits 0. Each scanner emits JSON to a file and human output to the
# terminal in one run:
#   snyk  -> --json-file-output (human stdout + json file)
#   trivy -> --format json --output, then `trivy convert` to render to stdout
#   grype -> -o table -o json=<file>
# Toggle scanners off (set to any non-empty value):
#   DISABLE_SNYK_SECURITY_SCAN, DISABLE_TRIVY_SECURITY_SCAN, DISABLE_GRYPE_SECURITY_SCAN
#
# Inputs from the Makefile targets:
#   GIT_COMMIT          short git hash, names the per-run folder
#   SCANS_DIR           root output directory (default: scans)
#   SCAN_IMAGES         space-separated "target-name=image-ref" pairs to scan (optional)
#   SCAN_EXCLUDE_DIRS   space-separated dir names excluded from the deps scan (optional)

set -o pipefail

SCANS_DIR="${SCANS_DIR:-scans}"
GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
TS="$(date -u '+%Y%m%dT%H%M%SZ')"
RUN_DIR="${SCANS_DIR}/${GIT_COMMIT}"

# Scan config — all repo-specific values come from env (see the Makefile's
# security-scan target); the script itself stays generic, no built-in defaults.
# SCAN_EXCLUDE_DIRS: space-separated dir names excluded from the deps scan, matched at any depth.
SCAN_EXCLUDE_DIRS="${SCAN_EXCLUDE_DIRS:-}"
# SCAN_IMAGES: space-separated "target-name=image-ref" pairs; empty refs are skipped.
read -ra SCAN_IMAGES <<< "${SCAN_IMAGES:-}"

# Minimum severity rendered in the condensed summary and compare output ("sev and
# above"); raw per-scanner JSON is always kept full. One of: critical|high|medium|low|all.
SCAN_SEV="${SCAN_SEV:-high}"
case "${SCAN_SEV}" in
  critical|high|medium|low|all) ;;
  *) echo "⚠️  invalid SCAN_SEV='${SCAN_SEV}' (expected critical|high|medium|low|all); defaulting to high" >&2; SCAN_SEV="high" ;;
esac

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
  echo "${RUN_DIR}/${1}_${2}.json"
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

# Populate the global array EXCLUDE_ARGS with scanner-specific flags built from
# SCAN_EXCLUDE_DIRS. Quoted array use at the call site keeps the **/ globs from
# being pathname-expanded by the shell.
build_exclude_args() {
  # build_exclude_args <snyk|trivy|grype>
  local dir csv=""
  EXCLUDE_ARGS=()
  case "$1" in
    snyk)
      for dir in ${SCAN_EXCLUDE_DIRS}; do csv="${csv:+${csv},}${dir}"; done
      [ -n "${csv}" ] && EXCLUDE_ARGS=(--exclude="${csv}") ;;
    trivy)
      for dir in ${SCAN_EXCLUDE_DIRS}; do EXCLUDE_ARGS+=(--skip-dirs "**/${dir}"); done ;;
    grype)
      for dir in ${SCAN_EXCLUDE_DIRS}; do EXCLUDE_ARGS+=(--exclude "**/${dir}/**"); done ;;
  esac
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
  # Excludes from SCAN_EXCLUDE_DIRS (deps come from the committed lockfile).
  # --json-file-output prints human-readable output to stdout AND writes JSON.
  build_exclude_args snyk
  snyk test --all-projects "${EXCLUDE_ARGS[@]}" --json-file-output="${deps_report}" || true
  [ -f "${deps_report}" ] && record "${deps_report}"

  local entry
  for entry in "${SCAN_IMAGES[@]}"; do
    scan_snyk_image "${entry#*=}" "${entry%%=*}"
  done
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
  # Excludes from SCAN_EXCLUDE_DIRS (deps come from the committed lockfile).
  build_exclude_args trivy
  trivy fs --scanners vuln "${EXCLUDE_ARGS[@]}" --format json --output "${deps_report}" . || true
  if [ -f "${deps_report}" ]; then
    record "${deps_report}"
    # Render the JSON report to the terminal without rescanning.
    trivy convert --format table "${deps_report}" || true
  fi

  local entry
  for entry in "${SCAN_IMAGES[@]}"; do
    scan_trivy_image "${entry#*=}" "${entry%%=*}"
  done
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
  # Excludes from SCAN_EXCLUDE_DIRS (deps come from the committed lockfile).
  # -o table streams to stdout; -o json=<file> writes the typed report.
  build_exclude_args grype
  grype dir:. "${EXCLUDE_ARGS[@]}" -o table -o "json=${deps_report}" || true
  [ -f "${deps_report}" ] && record "${deps_report}"

  local entry
  for entry in "${SCAN_IMAGES[@]}"; do
    scan_grype_image "${entry#*=}" "${entry%%=*}"
  done
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

# ── condense ───────────────────────────────────────────────────────────────--
# Merge this run's per-scanner reports into one deduplicated summary:
#   $RUN_DIR/security_scan.json  (machine-readable)
#   $RUN_DIR/security_scan.md    (grouped table)
# Dedup key: (target, package, installed-version, advisory). advisory prefers a
# CVE when the scanner exposes one (snyk/grype carry CVE aliases), else the
# primary id (GHSA / SNYK-...). Matching rows collapse across scanners, keeping
# the highest severity and first known fix. Cross-scanner dedup is approximate:
# a finding seen only as GHSA by one scanner and only as CVE by another (no
# shared alias) will not collapse.
condense() {
  if ! have jq; then
    warn "jq not installed; skipping condensed summary (per-scanner reports still written)"
    return 0
  fi
  shopt -s nullglob
  local reports=( "${RUN_DIR}"/*.json )
  [ "${#reports[@]}" -eq 0 ] && return 0

  CONDENSED_JSON="${RUN_DIR}/security_scan.json"
  CONDENSED_MD="${RUN_DIR}/security_scan.md"

  # Per-scanner flatten onto a uniform record:
  #   {scanner,target,advisory,package,installed,fixed,severity,title,url}
  local flatten
  read -r -d '' flatten <<'JQ'
def norm_sev:
  ascii_downcase
  | if . == "critical" then "critical"
    elif . == "high" then "high"
    elif . == "medium" or . == "moderate" then "medium"
    elif . == "low" or . == "negligible" then "low"
    else "unknown" end;
def nonempty: (. // "") | if . == "" then null else . end;
def flatten($scanner; $target):
  if $scanner == "trivy" then
    [ .Results[]? | (.Vulnerabilities // [])[] | {
        scanner:$scanner, target:$target, advisory:.VulnerabilityID,
        package:.PkgName, installed:.InstalledVersion, fixed:(.FixedVersion | nonempty),
        severity:((.Severity // "unknown") | norm_sev),
        title:((.Title // .Description // "")), url:(.PrimaryURL // "") } ]
  elif $scanner == "grype" then
    [ .matches[]? | (.vulnerability.id) as $pid
      | ([$pid] + [ .relatedVulnerabilities[]?.id ]) as $ids | {
          scanner:$scanner, target:$target,
          advisory:(($ids | map(select(type=="string" and startswith("CVE-"))) | .[0]) // $pid),
          package:.artifact.name, installed:.artifact.version,
          fixed:(((.vulnerability.fix.versions // []) | join(", ")) | nonempty),
          severity:((.vulnerability.severity // "unknown") | norm_sev),
          title:((.vulnerability.description // "")), url:(.vulnerability.dataSource // "") } ]
  elif $scanner == "snyk" then
    # container test => object with .vulnerabilities; test --all-projects => array of projects.
    (if type == "array" then . else [.] end)
    | [ .[].vulnerabilities[]? | ((.identifiers.CVE? // [])) as $cve | {
          scanner:$scanner, target:$target,
          advisory:(($cve | map(select(startswith("CVE-"))) | .[0]) // .id),
          package:.packageName, installed:.version,
          fixed:(((.fixedIn // []) | join(", ")) | nonempty),
          severity:((.severity // "unknown") | norm_sev),
          title:((.title // "")), url:("https://security.snyk.io/vuln/" + (.id // "")) } ]
  else [] end;
flatten($scanner; $target)[]
JQ

  local records="${WORKDIR}/records.ndjson"
  : > "${records}"
  local f base scanner target
  for f in "${reports[@]}"; do
    base="$(basename "${f}" .json)"          # <scanner>_<target>
    scanner="${base%%_*}"
    target="${base#*_}"
    case "${scanner}" in
      snyk|trivy|grype) ;;
      *) continue ;;                          # skip the condensed summary itself
    esac
    jq -c --arg scanner "${scanner}" --arg target "${target}" "${flatten}" "${f}" \
      >> "${records}" 2>/dev/null || true
  done

  jq -s --arg commit "${GIT_COMMIT}" --arg ts "${TS}" '
    def rank($s): {"critical":4,"high":3,"medium":2,"low":1,"unknown":0}[$s] // 0;
    def sevcount($f; $s): ($f | map(select(.severity == $s)) | length);
    . as $raw
    | ( $raw | group_by([.target, .package, .installed, .advisory])
        | map( . as $grp | {
            target:$grp[0].target, advisory:$grp[0].advisory,
            package:$grp[0].package, installed:$grp[0].installed,
            severity:($grp | max_by(rank(.severity)) | .severity),
            fixed:($grp | map(.fixed) | map(select(. != null)) | (.[0] // null)),
            scanners:($grp | map(.scanner) | unique),
            title:($grp | map(.title) | map(select(. != "")) | (.[0] // "")),
            url:($grp | map(.url) | map(select(. != "")) | (.[0] // ""))
          }) ) as $u
    | ( $u | sort_by([.target, (0 - rank(.severity)), .package]) ) as $findings
    | { commit:$commit, timestamp:$ts,
        totals: { raw:($raw|length), unique:($findings|length),
          critical:sevcount($findings;"critical"), high:sevcount($findings;"high"),
          medium:sevcount($findings;"medium"), low:sevcount($findings;"low"),
          unknown:sevcount($findings;"unknown") },
        by_target: ( $findings | group_by(.target)
          | map({ key:.[0].target, value:{ unique:length,
              critical:sevcount(.;"critical"), high:sevcount(.;"high"),
              medium:sevcount(.;"medium"), low:sevcount(.;"low"), unknown:sevcount(.;"unknown") }})
          | from_entries ),
        findings: $findings }
  ' "${records}" > "${CONDENSED_JSON}" || { warn "condense: failed to build summary JSON"; return 0; }

  jq -r --arg sev "${SCAN_SEV}" '
    def emoji($s): {"critical":"🔴","high":"🟠","medium":"🟡","low":"⚪","unknown":"❔"}[$s] // "❔";
    def rank($s): {"critical":4,"high":3,"medium":2,"low":1,"unknown":0}[$s] // 0;
    ({"critical":4,"high":3,"medium":2,"low":1,"all":0}[$sev] // 3) as $min
    | (.findings | map(select(rank(.severity) >= $min))) as $shown
    | "# Security scan summary", "",
    "Commit `\(.commit)` · \(.timestamp)", "",
    "**\(.totals.unique) unique findings** (from \(.totals.raw) raw rows) — " +
      "🔴 \(.totals.critical) critical · 🟠 \(.totals.high) high · 🟡 \(.totals.medium) medium · " +
      "⚪ \(.totals.low) low · ❔ \(.totals.unknown) unknown", "",
    "Showing **\($sev)+**: \($shown|length) of \(.totals.unique) (\((.totals.unique) - ($shown|length)) hidden below threshold)", "",
    ( $shown | group_by(.target)[] |
        "## \(.[0].target)", "",
        "| Sev | Advisory | Package | Installed | Fixed | Scanners |",
        "| --- | --- | --- | --- | --- | --- |",
        ( .[] | "| \(emoji(.severity)) \(.severity) | \(.advisory) | \(.package) | \(.installed) | \(.fixed // "—") | \(.scanners | join(", ")) |" ),
        "" )
  ' "${CONDENSED_JSON}" > "${CONDENSED_MD}" || { warn "condense: failed to render summary Markdown"; return 0; }
}

# ── compare ────────────────────────────────────────────────────────────────--
# Diff two runs' condensed summaries to show which findings were resolved vs
# introduced. Comparison key is (target, package, advisory) WITHOUT the
# installed version, so a version bump that drops a CVE reads as "resolved"
# rather than as a churned remove+add. Writes
# $SCANS_DIR/scan_compare_<before>_<after>.{json,md} and echoes the Markdown.
cmd_compare() {
  local before="$1" after="$2"
  if ! have jq; then
    echo "compare requires jq, which is not installed" >&2; exit 1
  fi
  if [ -z "${before}" ] || [ -z "${after}" ]; then
    echo "usage: security-scan.sh compare <before-hash> <after-hash>" >&2; exit 2
  fi
  local bj="${SCANS_DIR}/${before}/security_scan.json"
  local aj="${SCANS_DIR}/${after}/security_scan.json"
  for p in "${bj}" "${aj}"; do
    [ -f "${p}" ] || { echo "no condensed summary at ${p} (run 'make security-scan' on that commit)" >&2; exit 1; }
  done

  local out_json="${SCANS_DIR}/scan_compare_${before}_${after}.json"
  local out_md="${SCANS_DIR}/scan_compare_${before}_${after}.md"

  # Set math on (target, package, advisory): resolved = in before, gone in
  # after; introduced = in after, not before; persisting = in both.
  jq -n --slurpfile b "${bj}" --slurpfile a "${aj}" '
    def ckey: "\(.target) \(.package) \(.advisory)";
    def rank($s): {"critical":4,"high":3,"medium":2,"low":1,"unknown":0}[$s] // 0;
    ($b[0].findings // []) as $bf
    | ($a[0].findings // []) as $af
    | ([$bf[] | ckey] | unique) as $bk
    | ([$af[] | ckey] | unique) as $ak
    | (($ak | map({(.):true}) | add) // {}) as $akset
    | (($bk | map({(.):true}) | add) // {}) as $bkset
    | ([$bf[] | select($akset[ckey] | not)] | unique_by(ckey)
        | sort_by([.target, (0 - rank(.severity)), .package])) as $resolved
    | ([$af[] | select($bkset[ckey] | not)] | unique_by(ckey)
        | sort_by([.target, (0 - rank(.severity)), .package])) as $introduced
    | ([$af[] | select($bkset[ckey])] | unique_by(ckey)
        | sort_by([.target, (0 - rank(.severity)), .package])) as $persistingList
    | { before: $b[0].commit, after: $a[0].commit,
        counts: {
          resolved: ($resolved|length), introduced: ($introduced|length),
          persisting: ($persistingList|length), net: (($introduced|length) - ($resolved|length))
        },
        resolved: $resolved, introduced: $introduced, persisting_findings: $persistingList }
  ' > "${out_json}" || { echo "compare: failed to build ${out_json}" >&2; exit 1; }

  jq -r --arg sev "${SCAN_SEV}" '
    def sev($arr; $s): ($arr | map(select(.severity == $s)) | length);
    def emoji($s): {"critical":"🔴","high":"🟠","medium":"🟡","low":"⚪","unknown":"❔"}[$s] // "❔";
    def rank($s): {"critical":4,"high":3,"medium":2,"low":1,"unknown":0}[$s] // 0;
    def line: "| \(emoji(.severity)) \(.severity) | \(.target) | \(.advisory) | \(.package) | \(.installed) | \(.fixed // "—") |";
    ({"critical":4,"high":3,"medium":2,"low":1,"all":0}[$sev] // 3) as $min
    | (.resolved | map(select(rank(.severity) >= $min))) as $r
    | (.introduced | map(select(rank(.severity) >= $min))) as $i
    | ((.persisting_findings // []) | map(select(rank(.severity) >= $min))) as $p
    | "# Security scan compare", "",
      "`\(.before)` -> `\(.after)`", "",
      "Severity filter: **\($sev)+**", "",
      "Resolved:   \($r|length)  (🔴 \(sev($r;"critical")) · 🟠 \(sev($r;"high")) · 🟡 \(sev($r;"medium")) · ⚪ \(sev($r;"low")))",
      "Introduced: \($i|length)  (🔴 \(sev($i;"critical")) · 🟠 \(sev($i;"high")) · 🟡 \(sev($i;"medium")) · ⚪ \(sev($i;"low")))",
      "Unresolved: \($p|length)  (🔴 \(sev($p;"critical")) · 🟠 \(sev($p;"high")) · 🟡 \(sev($p;"medium")) · ⚪ \(sev($p;"low")))",
      "Net change: \(($i|length) - ($r|length))", "",
      (if ($r|length) > 0 then
        "## Resolved", "",
        "| Sev | Target | Advisory | Package | Installed (before) | Fixed |",
        "| --- | --- | --- | --- | --- | --- |",
        ($r[] | line), ""
       else "## Resolved", "", "_none_", "" end),
      (if ($i|length) > 0 then
        "## Introduced", "",
        "| Sev | Target | Advisory | Package | Installed (after) | Fixed |",
        "| --- | --- | --- | --- | --- | --- |",
        ($i[] | line), ""
       else "## Introduced", "", "_none_", "" end),
      (if ($p|length) > 0 then
        "## Unresolved", "",
        "| Sev | Target | Advisory | Package | Installed (after) | Fixed |",
        "| --- | --- | --- | --- | --- | --- |",
        ($p[] | line), ""
       else "## Unresolved", "", "_none_", "" end)
  ' "${out_json}" | tee "${out_md}"

  echo
  echo "Wrote ${out_json}"
  echo "Wrote ${out_md}"
}


# ── scan ───────────────────────────────────────────────────────────────────--
cmd_scan() {
  mkdir -p "${RUN_DIR}"
  echo "Security scan — commit ${GIT_COMMIT}, ${TS}"
  echo "Reports will be written to ${RUN_DIR}/"

  run_snyk
  run_trivy
  run_grype
  condense

  section "Summary"
  if [ "${#REPORTS[@]}" -eq 0 ]; then
    echo "No reports were produced (all scanners disabled, missing, or skipped)."
  else
    echo "Per-scanner reports:"
    for r in "${REPORTS[@]}"; do
      echo "  • ${r}"
    done
    if [ -n "${CONDENSED_JSON:-}" ] && [ -f "${CONDENSED_JSON}" ]; then
      echo "Condensed summary:"
      echo "  • ${CONDENSED_JSON}"
      echo "  • ${CONDENSED_MD}"
    fi
  fi
}

# ── dispatch ─────────────────────────────────────────────────────────────────

case "${1:-scan}" in
  scan)    cmd_scan ;;
  compare) shift; cmd_compare "$@" ;;
  *) echo "unknown subcommand '$1' (expected: scan | compare)" >&2; exit 2 ;;
esac
exit 0
