// Package importer is the v1alpha1-aware successor to
// internal/registry/importer. It reads user-authored manifest files,
// validates them, optionally runs security/health scanners, and writes
// v1alpha1 rows through the generic database.Store.
//
// See design-docs/V1ALPHA1_IMPORTER_ENRICHMENT.md for the full design
// including the Scanner plug-in contract, annotations/labels split,
// and the enrichment_findings table layout.
package importer

import (
	"context"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Scanner is the plug-in contract for security / health enrichment.
// OSS ships three built-in scanners (OSV, OpenSSF Scorecard, Trivy
// container scan); enterprise plugs in proprietary scanners by
// registering additional Scanner implementations into the importer.
//
// Lifecycle per-import:
//  1. importer decodes a resource
//  2. validates via v1alpha1.ValidateObject(obj)
//  3. asks each registered Scanner whether it Supports(obj)
//  4. calls Scan concurrently on supporting scanners
//  5. merges returned Annotations into ObjectMeta, appends Labels,
//     writes Findings rows to v1alpha1.enrichment_findings
//  6. Upserts the resource
//
// Scanner implementations are expected to own their own rate-limiting
// (external API quotas) and their own error-handling contract — any
// returned error is logged and surfaced as ImportResult.EnrichmentStatus
// but never aborts the import.
type Scanner interface {
	// Name is the short identifier used as the `source` value in the
	// enrichment_findings table. Stable across versions; clients may
	// filter by it. Examples: "osv", "scorecard", "container-scan".
	Name() string

	// Supports returns true when this scanner is applicable to obj.
	// OSV supports Agent + Skill + MCPServer; Scorecard supports any
	// kind with a Repository; scanners that don't apply to a kind
	// return false so the importer never invokes Scan on them.
	Supports(obj v1alpha1.Object) bool

	// Scan runs the scanner against obj and returns summary
	// annotations + labels plus per-finding detail. Scanner should
	// honor ctx for cancellation and apply its own rate-limiting.
	// Errors are logged and reported via ImportResult.EnrichmentStatus;
	// they do NOT abort the import of obj.
	Scan(ctx context.Context, obj v1alpha1.Object) (ScanResult, error)
}

// ScanResult is what a Scanner emits for a single object.
type ScanResult struct {
	// Annotations to merge into ObjectMeta.Annotations. Keys should
	// be fully qualified under the scanner's own prefix (e.g.
	// security.agentregistry.solo.io/osv-status).
	Annotations map[string]string

	// Labels to merge into ObjectMeta.Labels. Reserved for the small
	// set of "hot filter" keys promoted from annotations per the
	// enrichment design doc — typically osv-status, scorecard-bucket,
	// last-scanned-stale. Scanners should treat this as append-only
	// on the resource's label set.
	Labels map[string]string

	// Findings are full per-CVE / per-check entries persisted to
	// v1alpha1.enrichment_findings under source=Scanner.Name().
	// Each rescan atomically replaces the (object, source) set.
	Findings []Finding
}

// Finding is one row in the enrichment_findings table.
type Finding struct {
	// Severity buckets for filtering + count rollups into the
	// summary annotations.
	Severity string // "critical" | "high" | "medium" | "low" | "none"

	// ID is the scanner-specific identifier (CVE-2024-12345,
	// scorecard check name, Trivy rule ID). Must be stable so
	// replace-on-rescan matches prior entries by ID when needed.
	ID string

	// Data is the raw scanner payload — CVSS vectors, remediation
	// URLs, scorecard details, Trivy layer info, etc. Preserved as
	// JSONB in the DB for audit queries.
	Data map[string]any

	// When the scanner produced this finding. Defaults to Scan call
	// time when zero.
	FoundAt time.Time
}

// -----------------------------------------------------------------------------
// Known annotation + label keys (the enrichment vocabulary).
// -----------------------------------------------------------------------------

// Prefix for every enrichment annotation + label key. Matches
// Kubernetes convention: domain-qualified so scanners from different
// vendors don't collide.
const EnrichmentPrefix = "security.agentregistry.solo.io/"

const (
	// Summary annotations (all scanners).
	AnnoLastScannedAt    = EnrichmentPrefix + "last-scanned-at"
	AnnoLastScannedBy    = EnrichmentPrefix + "last-scanned-by"
	AnnoLastScannedStale = EnrichmentPrefix + "last-scanned-stale"

	// OSV scanner output.
	AnnoOSVStatus        = EnrichmentPrefix + "osv-status" // clean | vulnerable | unknown
	AnnoOSVCountCritical = EnrichmentPrefix + "osv-count-critical"
	AnnoOSVCountHigh     = EnrichmentPrefix + "osv-count-high"
	AnnoOSVCountMedium   = EnrichmentPrefix + "osv-count-medium"
	AnnoOSVCountLow      = EnrichmentPrefix + "osv-count-low"

	// OpenSSF Scorecard output.
	AnnoScorecardScore  = EnrichmentPrefix + "scorecard-score"
	AnnoScorecardRef    = EnrichmentPrefix + "scorecard-ref"
	AnnoScorecardBucket = EnrichmentPrefix + "scorecard-bucket" // A|B|C|D|F

	// Trivy container scan.
	AnnoContainerStatus        = EnrichmentPrefix + "container-scan-status"
	AnnoContainerCountCritical = EnrichmentPrefix + "container-scan-count-critical"
	AnnoContainerCountHigh     = EnrichmentPrefix + "container-scan-count-high"
)

// Per the design doc's "promote hot keys to labels" decision, exactly
// three keys live in BOTH annotations (for drill-down) AND labels (for
// GIN-indexed filtering). Scanners write them in both places; the
// importer's merge logic writes them once in each column.
var PromotedToLabels = []string{
	AnnoOSVStatus,
	AnnoScorecardBucket,
	AnnoLastScannedStale,
}
