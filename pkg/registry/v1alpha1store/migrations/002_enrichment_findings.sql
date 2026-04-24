-- v1alpha1 enrichment findings: detailed per-scan results attached to
-- any v1alpha1 resource via (kind, namespace, name, version).
--
-- Summary annotations live on the resource's ObjectMeta.Annotations
-- under the security.agentregistry.solo.io/* prefix. This table holds
-- the full per-CVE / per-check detail for audit queries and UI
-- drill-down. Not FK'd to the resource tables because findings may
-- outlive the resource (compliance audit trail); application logic
-- replaces the set atomically per (object, source) on every rescan.
--
-- See design-docs/V1ALPHA1_IMPORTER_ENRICHMENT.md for full rationale.

CREATE TABLE IF NOT EXISTS v1alpha1.enrichment_findings (
    id          BIGSERIAL PRIMARY KEY,

    -- Resource this finding attaches to. (kind, namespace, name, version)
    -- intentionally loose — we don't FK to the resource tables so
    -- findings can remain for audit after a resource row is purged.
    kind        VARCHAR(50)  NOT NULL,  -- "Agent" | "MCPServer" | ...
    namespace   VARCHAR(255) NOT NULL,
    name        VARCHAR(255) NOT NULL,
    version     VARCHAR(255) NOT NULL,

    -- Scanner identity + severity/ID for filtering.
    source      VARCHAR(50)  NOT NULL,  -- "osv" | "scorecard" | "container-scan" | enterprise-added
    severity    VARCHAR(20),            -- "critical" | "high" | "medium" | "low" | "none"
    finding_id  TEXT,                   -- "CVE-2024-12345" | "Branch-Protection" | opaque per source

    -- Source-specific full payload (CVSS vectors, remediation links,
    -- scorecard check details, trivy layer info, etc.).
    data        JSONB NOT NULL,

    -- Provenance.
    scanned_by  VARCHAR(255),           -- "importer-cli" | "reconciler-cron" | actor name
    found_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Lookup by resource is the hot path (UI "show findings for this MCPServer").
CREATE INDEX IF NOT EXISTS enrichment_findings_obj
    ON v1alpha1.enrichment_findings (kind, namespace, name, version);

-- Allow "show me all vulnerable rows discovered by OSV across namespace X".
CREATE INDEX IF NOT EXISTS enrichment_findings_source
    ON v1alpha1.enrichment_findings (source);

-- "Show me critical findings" queries.
CREATE INDEX IF NOT EXISTS enrichment_findings_severity
    ON v1alpha1.enrichment_findings (severity);

-- Sweep-by-time queries for audits and GC of ancient findings.
CREATE INDEX IF NOT EXISTS enrichment_findings_found_at
    ON v1alpha1.enrichment_findings (found_at DESC);
