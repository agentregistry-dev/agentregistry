package v1alpha1store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// rowScanner is anything that Scan()s a single row — both pgx.Row and
// pgx.Rows satisfy it.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRow reads one row worth of columns (in the order emitted by Get/List
// queries) into a v1alpha1.RawObject. Spec and Status are retained as their
// wire-form representations so callers can unmarshal into typed structs.
//
// tagged reflects the Store's private behavior and decides whether the scanned
// tag column should populate public metadata.tag. Mutable-object queries
// emit an empty synthetic value to keep the column layout uniform.
// Tagged content queries emit a synthetic 0::bigint generation and '[]'::jsonb
// finalizers so the column layout stays uniform across modes.
//
// Column order must match:
//
//	namespace, name, tag-or-empty, uid, generation, labels, annotations, spec, status,
//	deletion_timestamp, finalizers, created_at, updated_at
func scanRow(row rowScanner, tagged bool) (*v1alpha1.RawObject, error) {
	var (
		namespace         string
		name              string
		tag               string
		uid               string
		generation        int64
		labelsJSON        []byte
		annotationsJSON   []byte
		specJSON          []byte
		statusJSON        []byte
		deletionTimestamp *time.Time
		finalizersJSON    []byte
		createdAt         time.Time
		updatedAt         time.Time
	)

	if err := row.Scan(
		&namespace, &name, &tag, &uid, &generation,
		&labelsJSON, &annotationsJSON, &specJSON, &statusJSON,
		&deletionTimestamp, &finalizersJSON,
		&createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pkgdb.ErrNotFound
		}
		return nil, fmt.Errorf("scan row: %w", err)
	}

	var labels map[string]string
	if len(labelsJSON) > 0 {
		if err := json.Unmarshal(labelsJSON, &labels); err != nil {
			return nil, fmt.Errorf("decode labels: %w", err)
		}
	}

	var annotations map[string]string
	if len(annotationsJSON) > 0 {
		if err := json.Unmarshal(annotationsJSON, &annotations); err != nil {
			return nil, fmt.Errorf("decode annotations: %w", err)
		}
	}

	// finalizersJSON intentionally not parsed onto ObjectMeta — there
	// is no public API for finalizers anymore.
	_ = finalizersJSON

	meta := v1alpha1.ObjectMeta{
		Namespace:         namespace,
		Name:              name,
		UID:               uid,
		Labels:            labels,
		Annotations:       annotations,
		Generation:        generation,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		DeletionTimestamp: deletionTimestamp,
	}
	raw := &v1alpha1.RawObject{
		Metadata: meta,
		Spec:     json.RawMessage(specJSON),
		Status:   json.RawMessage(statusJSON),
	}
	if tagged {
		meta.Tag = tag
		raw.Metadata = meta
	}

	return raw, nil
}

// runInTx executes fn within a read-committed transaction, committing on nil
// return and rolling back on error.
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
