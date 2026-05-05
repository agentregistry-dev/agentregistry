package v1alpha1store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
// versioned reflects the Store's table mode and decides whether the
// version column should be scanned as int or string. Versioned-artifact
// queries emit a synthetic 0::bigint generation and '[]'::jsonb
// finalizers so the column layout stays uniform across modes.
//
// Column order must match:
//
//	namespace, name, version, generation, labels, annotations, spec, status,
//	deletion_timestamp, finalizers, created_at, updated_at
func scanRow(row rowScanner, versioned bool) (*v1alpha1.RawObject, error) {
	var (
		namespace         string
		name              string
		versionInt        int
		versionStr        string
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

	var versionDest any
	if versioned {
		versionDest = &versionInt
	} else {
		versionDest = &versionStr
	}
	if err := row.Scan(
		&namespace, &name, versionDest, &generation,
		&labelsJSON, &annotationsJSON, &specJSON, &statusJSON,
		&deletionTimestamp, &finalizersJSON,
		&createdAt, &updatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pkgdb.ErrNotFound
		}
		return nil, fmt.Errorf("scan row: %w", err)
	}

	versionRendered := versionStr
	if versioned {
		versionRendered = strconv.Itoa(versionInt)
	}

	return decodeRow(
		namespace, name, versionRendered, generation,
		labelsJSON, annotationsJSON, specJSON, statusJSON,
		deletionTimestamp, finalizersJSON, createdAt, updatedAt,
	)
}

// decodeRow builds a RawObject from already-scanned column values. Split
// from scanRow so callers that scan extra trailing columns (SemanticList's
// distance score) can reuse the deserialization without repeating its
// logic.
func decodeRow(
	namespace, name, version string,
	generation int64,
	labelsJSON, annotationsJSON, specJSON, statusJSON []byte,
	deletionTimestamp *time.Time,
	finalizersJSON []byte,
	createdAt, updatedAt time.Time,
) (*v1alpha1.RawObject, error) {
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

	return &v1alpha1.RawObject{
		Metadata: v1alpha1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			Version:           version,
			Labels:            labels,
			Annotations:       annotations,
			Generation:        generation,
			CreatedAt:         createdAt,
			UpdatedAt:         updatedAt,
			DeletionTimestamp: deletionTimestamp,
		},
		Spec:   json.RawMessage(specJSON),
		Status: json.RawMessage(statusJSON),
	}, nil
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
