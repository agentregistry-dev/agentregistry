# v1alpha1 Importer + Security Enrichment — Design Draft

> Status: **draft — not implemented yet**.
>
> Scope: port the legacy importer (`internal/registry/importer/`,
> ~2.5k LOC) to v1alpha1 types + Store. Decide where security
> enrichment findings (OSV scan, OpenSSF scorecard, container scan)
> attach on a v1alpha1 object and how the data flows between import,
> storage, and query.

---

## What exists today

`internal/registry/importer/importer.go` (1535 LOC) + supporting files:
- **importer.go**: reads manifests from a path/URL, dedups by name,
  translates to `apiv0.ServerJSON`, calls `serversvc.PublishServer`.
- **container_scan.go** (187 LOC): shells out to Trivy for image CVEs.
- **osv_scan.go** (307 LOC): queries OSV.dev for package
  vulnerabilities.
- **scorecard_lib.go** (128 LOC): wraps OpenSSF Scorecard library for
  repository health scoring.
- **dependency_health.go** (212 LOC): endpoint reachability + uptime
  probes.

Legacy enrichment attaches to `_meta.publisherProvided` inside
ServerJSON — a free-form JSONB blob. Not queryable; fine for UI
display.

---

## Goals

1. Port import flow to produce `*v1alpha1.MCPServer` (and eventually
   Agent/Skill/Prompt) rows via the generic Store.
2. Carry enrichment findings into a queryable shape so the UI can
   filter by "clean" vs "has CVEs" and sort by scorecard.
3. Keep the enrichment tools themselves as-is — OSV API call logic
   isn't the interesting redesign; their attachment surface is.
4. Run enrichment on-demand (opt-in `--enrich` flag at import) and
   optionally periodically (reconciler cron).

## Non-goals (this PR)

- Inventing a new vulnerability database or scorecard.
- Real-time CVE webhook subscriptions.

---

## Proposed attachment surface

### Add `ObjectMeta.Annotations`

K8s-idiomatic free-form key-value map for controller/tool state.
Differs from labels: labels are queryable (GIN-indexed, small); annotations
are narrative (large blobs OK, not indexed).

```go
type ObjectMeta struct {
    Namespace, Name, Version string
    Labels                   map[string]string
    Annotations              map[string]string   // NEW
    // ...rest unchanged
}
```

DB: `annotations JSONB NOT NULL DEFAULT '{}'::jsonb` on every table.
Not GIN-indexed (not queryable by default).

### Enrichment annotation namespace

All enrichment state under `security.agentregistry.solo.io/`:

| Key | Example value | Notes |
|---|---|---|
| `security.agentregistry.solo.io/osv-status` | `clean` / `vulnerable` / `unknown` | Summary for filtering |
| `security.agentregistry.solo.io/osv-count-critical` | `2` | Count by severity for sorting |
| `security.agentregistry.solo.io/osv-count-high` | `5` |  |
| `security.agentregistry.solo.io/osv-count-medium` | `12` |  |
| `security.agentregistry.solo.io/osv-count-low` | `0` |  |
| `security.agentregistry.solo.io/scorecard-score` | `8.5` | OpenSSF scorecard 0-10 |
| `security.agentregistry.solo.io/scorecard-ref` | `sha256:abc` | Commit the score was calculated against |
| `security.agentregistry.solo.io/container-scan-status` | `clean` / `vulnerable` / `unknown` | Trivy summary |
| `security.agentregistry.solo.io/container-scan-count-critical` | `0` |  |
| `security.agentregistry.solo.io/last-scanned-at` | `2026-04-17T16:00:00Z` | RFC3339 |
| `security.agentregistry.solo.io/last-scanned-by` | `importer-cli` / `reconciler-cron` | Provenance |

Rationale: scalar annotations are cheap to read and render; the UI
shows the summary. A user asking "give me all MCPServers with
critical CVEs" does `labels.security-osv-status=vulnerable`
(promoting `osv-status` to a label at write-time if the UI/API
filter path needs it — see open question #2).

### Detailed findings table

Full per-finding detail (every CVE ID, every scorecard check) in a
side table — too much data for annotations, needs structured
columns for audit queries.

```sql
CREATE TABLE v1alpha1.enrichment_findings (
    id             BIGSERIAL PRIMARY KEY,
    kind           VARCHAR(50)  NOT NULL,      -- e.g. "MCPServer"
    namespace      VARCHAR(255) NOT NULL,
    name           VARCHAR(255) NOT NULL,
    version        VARCHAR(255) NOT NULL,
    source         VARCHAR(50)  NOT NULL,      -- "osv" | "scorecard" | "container-scan"
    severity       VARCHAR(20),                -- "critical" | "high" | "medium" | "low" | "none"
    finding_id     TEXT,                       -- "CVE-2024-12345" | "Branch-Protection"
    data           JSONB        NOT NULL,      -- source-specific full payload
    found_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    scanned_by     VARCHAR(255)                -- same provenance as annotation
);
CREATE INDEX enrichment_findings_obj     ON v1alpha1.enrichment_findings (kind, namespace, name, version);
CREATE INDEX enrichment_findings_source  ON v1alpha1.enrichment_findings (source);
CREATE INDEX enrichment_findings_severity ON v1alpha1.enrichment_findings (severity);
```

No FK to the resource tables — the table cascades don't, because
findings might outlive the resource (audit). Application logic
deletes findings for a (kind,namespace,name,version) tuple when a
new scan replaces them.

### What goes on the annotation vs findings table

- **Annotations**: scalar summaries, counts, status — what the UI
  displays without clicking into detail. ~10 keys per object.
- **Findings table**: full per-CVE / per-check detail for audit and
  drill-down UI. Unbounded per object.

Replace-on-rescan: when a new scan runs, annotations are overwritten
and prior findings for that (object, source) are deleted, then new
findings inserted. Keeps the table bounded and the annotation
summaries fresh.

---

## Porting the importer

### Phase 1: importer-core port

```go
// pkg/importer/importer.go (new location, moved out of internal)
package importer

type Options struct {
    // Path to a directory or a manifest file (YAML/JSON), or URL.
    Path string
    // Namespace to import into. Required.
    Namespace string
    // Enrich triggers security enrichment per imported object.
    Enrich bool
    // WhichScans narrows enrichment; empty = all.
    WhichScans []ScanType
    // DryRun prints the plan without writing.
    DryRun bool
}

type ImportResult struct {
    Kind, Namespace, Name, Version string
    Status                         string // "created" | "updated" | "unchanged" | "failed"
    EnrichmentStatus               string // "skipped" | "ok" | "partial" | "failed"
    Error                          string
}

func ImportFromPath(ctx context.Context, stores map[string]*database.Store, opts Options) ([]ImportResult, error)
```

Behavior:
1. Walk `opts.Path`, decode each YAML/JSON file with
   `v1alpha1.Scheme.DecodeMulti`. Only v1alpha1 content accepted
   (drop the legacy ServerJSON translation — that lived inside
   PublishServer; new path uses `Store.Upsert` directly).
2. For each decoded object:
   - Validate via `obj.Validate()`.
   - If `opts.Enrich`: dispatch scanners by object kind.
   - Merge enrichment annotations into ObjectMeta.Annotations.
   - Upsert the row.
   - Write detail findings to `v1alpha1.enrichment_findings`.

### Phase 2: scanner plug-ins

Scanners implement a narrow interface so new sources can register.

```go
type Scanner interface {
    Name() string                                  // "osv", "scorecard", "container-scan"
    Supports(obj v1alpha1.Object) bool             // applicability
    Scan(ctx context.Context, obj v1alpha1.Object) (ScanResult, error)
}

type ScanResult struct {
    Annotations map[string]string  // summary keys under security.agentregistry.solo.io/
    Findings    []Finding           // detailed entries for the findings table
}

type Finding struct {
    Severity string
    ID       string
    Data     map[string]any
}
```

Built-ins (all reimplementations of the legacy packages, keeping their
core logic):
- `scanners/osv.go`: OSV API query for package + version.
- `scanners/scorecard.go`: OpenSSF scorecard against `Repository.URL`.
- `scanners/container.go`: Trivy shell-out for image packages.

Register at init time; `Options.WhichScans` gates by name.

### Phase 3: reconciler-triggered rescans (optional, followup)

A background worker re-runs scanners periodically. Scope:
- Pick objects whose `last-scanned-at` annotation is older than a
  configurable threshold (e.g. 7 days).
- Run scanners; update annotations + findings.

Keeps findings fresh without a user having to re-run `arctl import`.
Can wait for the KRT reconciler (Group 2).

---

## Resolutions (2026-04-17)

All 8 questions reviewed; all resolved below. The Open questions
section that follows preserves the trade-off discussion.

1. **Add ObjectMeta.Annotations → land now as its own small PR**.
   Separate additive design-PR (`annotations JSONB NOT NULL DEFAULT
   '{}'::jsonb` on all 6 tables; `Annotations map[string]string` on
   ObjectMeta; round-trip tests). Unblocks platform-adapter
   ProviderMetadata and enrichment annotations in one go.

2. **Promote hot enrichment annotations to labels for query speed →
   yes, 3 keys**. Importer writes these to both annotations AND
   labels:
   - `security.agentregistry.solo.io/osv-status` = `clean|vulnerable|unknown`
   - `security.agentregistry.solo.io/scorecard-bucket` = `A|B|C|D|F` (bucketed score)
   - `security.agentregistry.solo.io/last-scanned-stale` = `true|false`
   UI + API filters hit the GIN-indexed labels column; details pulled
   from annotations on drill-down.

3. **Findings table → OSS, open to enterprise-added scanners**.
   `v1alpha1.enrichment_findings` lives in OSS migrations. Enterprise
   proprietary scanners register via the Scanner plug-in interface
   and write to the same table under their own `source` value.

4. **Importer trust model → always validate, no bypass**. User-
   authored manifests imported via `arctl import` always run
   `obj.Validate()`. Broken manifests surface at import time as an
   `ImportResult.Status=failed`. Seed-style bypass stays server-side
   only (seed is curated, imports aren't).

5. **Stale findings removal → DELETE + INSERT per scan in one tx**.
   When a scan produces a new set of findings for (object, source),
   the old findings for that pair are deleted and new ones inserted
   in a single transaction. Keeps the table bounded and the rows
   authoritative as of the last scan.

6. **Per-kind applicability → scanners self-declare via
   `Supports(obj)`**. OSV supports Agent + Skill + MCPServer;
   Scorecard supports anything with Repository; Prompt gets skipped
   silently. Scanners that don't support a kind return `false` and
   are never invoked.

7. **Rate-limiting → scanner-internal**. Each scanner owns its own
   `golang.org/x/time/rate` bucket tuned to the target external
   API's published limits. Importer doesn't coordinate; bursty
   imports just serialize inside each scanner.

8. **Failure isolation → non-fatal, logged, carried on
   ImportResult**. `ImportResult.EnrichmentStatus` is
   `ok|partial|failed`; importer always proceeds to Upsert the
   object even if scanners errored. Per-scanner errors aggregated
   into `EnrichmentErrors []string`. UI shows "scan failed, retry
   later" instead of blocking the import.

---

## Open questions (for reference)

### 1. Where does `ObjectMeta.Annotations` go on the wire?

Adding a new ObjectMeta field is a spec change. We already did
Namespace + DeletionTimestamp + Finalizers that way; Annotations
should be uncontroversial if K8s-aligned. Adding 1 column to 6
tables + 1 struct field.

Recommendation: **add in a small design-PR of its own** once this
importer draft is approved; gives enrichment a home without rushing
it.

### 2. Should enrichment annotations be promoted to labels for filter queries?

E.g. `security.agentregistry.solo.io/osv-status=vulnerable` — if
this is the single most common filter, putting it in labels gets
GIN-index speed.

Options:
- **a)** Everything stays in annotations; querying uses JSONB
  containment on the annotations column. Works but slower on large
  datasets.
- **b)** Promote 2-3 "hot" summary keys to labels. Users still read
  them from annotations; the importer writes them in both places.
  Duplication is acceptable given read patterns.
- **c)** Dedicated spec extension (`MCPServerSpec.Security
  SecuritySummary`). Schema-typed; no duplication. Couples spec
  evolution to enrichment though.

Recommendation: **(b)**. Promote `osv-status`, `scorecard-score`
(bucketed), and `last-scanned-at` to labels. UI filters on labels;
detailed rendering reads annotations.

### 3. Who owns the findings table? OSS or enterprise?

The enrichment scanners live in OSS (Trivy, OSV, Scorecard are all
public tools). The table can be OSS.

Enterprise can add its own scanners (proprietary SAST, license
scanners, etc.) via the Scanner plug-in interface. They write to the
same findings table under their own `source` value.

Recommendation: **OSS table, open to enterprise-added scanners**.

### 4. How does the importer trust manifest files?

`arctl import --from ./manifests/` today trusts files on disk. If
the importer bypasses `Validate()` the way seed does, users can
inject garbage that later fails at query time.

Options:
- **a)** Import path *always* runs `obj.Validate()` — no
  trusted-source bypass. Seed stays separate (and keeps bypassing).
- **b)** Import adds a `--trusted` flag that bypasses validation
  (for curated imports).

Recommendation: **(a)**. User-authored files should be validated;
seed-style bypass is for server-side curated content only.

### 5. What about removing stale findings?

When a CVE is fixed, a rescan should remove the finding. Strategy:

```sql
BEGIN;
DELETE FROM v1alpha1.enrichment_findings
 WHERE kind=$1 AND namespace=$2 AND name=$3 AND version=$4
   AND source=$5;
INSERT INTO v1alpha1.enrichment_findings (...) VALUES (...), (...);
UPDATE v1alpha1.<table> SET annotations = jsonb_set(...);
COMMIT;
```

One transaction per scan pass. Straightforward; tracked here for
completeness.

### 6. Agent + Skill + Prompt enrichment

Current importer + scanners are MCPServer-centric (OSV for server
packages, scorecard for server repo, container scan for server
image). For Agent / Skill / Prompt:

- **Agent**: has `Packages` + `Repository` too — scorecard applies,
  maybe OSV.
- **Skill**: similar — `Packages`.
- **Prompt**: no packages / repo. Skip enrichment.

Recommendation: **scanners self-declare applicability via
Scanner.Supports**. OSV supports Agent + Skill + MCPServer;
Scorecard supports anything with Repository; Prompt gets skipped
silently.

### 7. Rate-limiting external scanners

OSV.dev + scorecard have rate limits. A bulk import of 300 MCPServers
can hammer them.

Recommendation: **scanners apply their own rate-limiting** (e.g.
`golang.org/x/time/rate` bucket inside osv.go). Importer doesn't
need to know.

### 8. Failure isolation

Scanner failures shouldn't fail the import — they're enrichment,
not core.

Recommendation: **ImportResult.EnrichmentStatus** carries
`ok|partial|failed`; importer always proceeds to Upsert even if
some scanners errored. Errors logged, not fatal.

---

## Port sequence

1. **Add `ObjectMeta.Annotations`** — small additive design-PR
   (mirrors Namespace + DeletionTimestamp pattern; 1 column per
   table, 1 struct field, tests update).
2. **Create `v1alpha1.enrichment_findings` table** — additive
   migration.
3. **Port OSV scanner** (smallest scanner, ~300 LOC; port 1:1 on
   the new Scanner interface).
4. **Port Scorecard scanner**.
5. **Port container scanner**.
6. **Write new importer core** that uses `v1alpha1.Scheme.DecodeMulti`
   and the generic Store.
7. **CLI/server wiring** — once the declarative CLI lands, wire
   `arctl import` to the new importer.
8. **Retire legacy** — delete `internal/registry/importer/` once no
   callers remain (seed doesn't use it; only the legacy `arctl
   import` + `arctl cli/import.go` did).

Estimated effort: **3-4 days** for the port; **plus the Annotations
spec change** as its own day.

---

## What this unblocks

- User-authored manifest import with v1alpha1 envelopes.
- UI "security scan results" feature (works off the annotations +
  findings table).
- Scheduled rescans (via reconciler Group 2).
- Third-party / enterprise scanner extensions.
