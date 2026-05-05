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

	return decodeRow(
		versioned,
		namespace, name, versionInt, versionStr,
		labelsJSON, annotationsJSON, specJSON, statusJSON,
		deletionTimestamp, finalizersJSON, createdAt, updatedAt,
	)
}

// decodeRow builds a RawObject from already-scanned column values. Split
// from scanRow so callers that scan extra trailing columns (SemanticList's
// distance score) can reuse the deserialization without repeating its
// logic.
//
// Both modes populate Metadata.Version: it's the row's PK identifier
// rendered as a string, so wire clients (CLI, e2e tests) can use it
// without consulting Status. Versioned-artifact rows additionally fold
// the integer into Status.Version — the canonical surface new code
// should read for system-assigned versions. Status.Version stays zero
// for legacy deployments since they have no integer counterpart.
func decodeRow(
	versioned bool,
	namespace, name string,
	versionInt int, versionStr string,
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

	meta := v1alpha1.ObjectMeta{
		Namespace:         namespace,
		Name:              name,
		Labels:            labels,
		Annotations:       annotations,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		DeletionTimestamp: deletionTimestamp,
	}
	if versioned {
		meta.Version = strconv.Itoa(versionInt)
	} else {
		meta.Version = versionStr
	}

	// For versioned-artifact rows fold the row's integer version into
	// the status payload so callers see status.version reflect the
	// just-read row. Legacy deployment rows have no system-assigned
	// integer version, so we leave the status payload alone.
	finalStatus := json.RawMessage(statusJSON)
	if versioned {
		merged, err := mergeStatusVersion(statusJSON, versionInt)
		if err != nil {
			return nil, fmt.Errorf("merge status.version: %w", err)
		}
		finalStatus = merged
	}

	return &v1alpha1.RawObject{
		Metadata: meta,
		Spec:     json.RawMessage(specJSON),
		Status:   finalStatus,
	}, nil
}

// mergeStatusVersion sets Status.Version to v on the provided JSONB
// payload, preserving any other fields (conditions, etc.) that the
// reconciler wrote. An empty input produces a fresh status with only
// Version set.
func mergeStatusVersion(statusJSON []byte, v int) ([]byte, error) {
	var s v1alpha1.Status
	if err := v1alpha1.UnmarshalStatusFromStorage(statusJSON, &s); err != nil {
		return nil, err
	}
	s.Version = v
	return v1alpha1.MarshalStatusForStorage(s)
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
