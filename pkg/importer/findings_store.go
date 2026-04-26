package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// FindingsStore is the thin persistence wrapper around
// v1alpha1.enrichment_findings. It exposes the two operations the
// importer needs: atomic replace-on-rescan for a (resource, source)
// pair, and a read for UI drill-down queries.
//
// The importer creates one FindingsStore per pool and passes it into
// Scanner invocations indirectly — scanners return Findings and the
// importer writes them here.
type FindingsStore struct {
	pool *pgxpool.Pool
}

// NewFindingsStore wraps pool in a FindingsStore.
func NewFindingsStore(pool *pgxpool.Pool) *FindingsStore {
	return &FindingsStore{pool: pool}
}

// Replace atomically deletes every finding for (object, source) and
// inserts the supplied set. Idempotent; safe to call with an empty
// findings slice (which just clears the prior set for that source).
//
// Run inside a single transaction so UI queries never see a partial
// rescan state.
func (s *FindingsStore) Replace(
	ctx context.Context,
	kind, namespace, name, version, source, scannedBy string,
	findings []Finding,
) error {
	return runInTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			DELETE FROM v1alpha1.enrichment_findings
			WHERE kind=$1 AND namespace=$2 AND name=$3 AND version=$4 AND source=$5
		`, kind, namespace, name, version, source); err != nil {
			return fmt.Errorf("delete prior findings: %w", err)
		}
		for _, f := range findings {
			data := f.Data
			if data == nil {
				data = map[string]any{}
			}
			raw, err := json.Marshal(data)
			if err != nil {
				return fmt.Errorf("marshal finding data: %w", err)
			}
			foundAt := f.FoundAt
			if foundAt.IsZero() {
				foundAt = time.Now().UTC()
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO v1alpha1.enrichment_findings
				    (kind, namespace, name, version, source, severity, finding_id, data, scanned_by, found_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			`,
				kind, namespace, name, version, source,
				f.Severity, f.ID, raw, scannedBy, foundAt,
			); err != nil {
				return fmt.Errorf("insert finding: %w", err)
			}
		}
		return nil
	})
}

// List returns every finding for a (kind, namespace, name, version).
// Used by the UI drill-down "show me findings" view. Ordered by
// severity DESC then found_at DESC.
func (s *FindingsStore) List(
	ctx context.Context,
	kind, namespace, name, version string,
) ([]Finding, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT severity, finding_id, data, found_at
		FROM v1alpha1.enrichment_findings
		WHERE kind=$1 AND namespace=$2 AND name=$3 AND version=$4
		ORDER BY
		    CASE severity
		        WHEN 'critical' THEN 0
		        WHEN 'high'     THEN 1
		        WHEN 'medium'   THEN 2
		        WHEN 'low'      THEN 3
		        ELSE             4
		    END,
		    found_at DESC
	`, kind, namespace, name, version)
	if err != nil {
		return nil, fmt.Errorf("query findings: %w", err)
	}
	defer rows.Close()

	out := make([]Finding, 0, 16)
	for rows.Next() {
		var (
			severity string
			id       string
			raw      []byte
			foundAt  time.Time
		)
		if err := rows.Scan(&severity, &id, &raw, &foundAt); err != nil {
			return nil, fmt.Errorf("scan finding: %w", err)
		}
		var data map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &data); err != nil {
				return nil, fmt.Errorf("decode finding data: %w", err)
			}
		}
		out = append(out, Finding{
			Severity: severity,
			ID:       id,
			Data:     data,
			FoundAt:  foundAt,
		})
	}
	return out, rows.Err()
}

// ListByObject is a convenience wrapper over List that accepts a
// v1alpha1.Object instead of the four identity strings.
func (s *FindingsStore) ListByObject(ctx context.Context, obj v1alpha1.Object) ([]Finding, error) {
	m := obj.GetMetadata()
	return s.List(ctx, obj.GetKind(), m.Namespace, m.Name, m.Version)
}

// runInTx is a local copy of the database package helper; we don't
// import internal/registry/database to keep this package's import
// surface narrow.
func runInTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
