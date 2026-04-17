package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
func scanRow(row rowScanner) (*v1alpha1.RawObject, error) {
	var (
		name       string
		version    string
		generation int64
		labelsJSON []byte
		specJSON   []byte
		statusJSON []byte
		createdAt  time.Time
		updatedAt  time.Time
	)
	if err := row.Scan(&name, &version, &generation, &labelsJSON, &specJSON, &statusJSON, &createdAt, &updatedAt); err != nil {
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

	var status v1alpha1.Status
	if len(statusJSON) > 0 {
		if err := json.Unmarshal(statusJSON, &status); err != nil {
			return nil, fmt.Errorf("decode status: %w", err)
		}
	}

	return &v1alpha1.RawObject{
		Metadata: v1alpha1.ObjectMeta{
			Name:       name,
			Version:    version,
			Labels:     labels,
			Generation: generation,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		},
		Spec:   json.RawMessage(specJSON),
		Status: status,
	}, nil
}

// normalizeJSON re-marshals a JSON document so byte-level equality reflects
// semantic equality (key order, whitespace). Used by Upsert's spec-change
// detection so that re-serialized input doesn't falsely bump generation.
func normalizeJSON(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return b
	}
	out, err := json.Marshal(v)
	if err != nil {
		return b
	}
	return out
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

// join is a package-local strings.Join to avoid importing strings in the
// main store file; we only need this one helper.
func join(parts []string, sep string) string { return strings.Join(parts, sep) }

// compileAssertions keeps the unused-import complaint quiet when bytes and
// pgxpool are referenced indirectly.
var _ = bytes.Equal
var _ *pgxpool.Pool
