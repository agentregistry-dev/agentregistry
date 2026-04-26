package v1alpha1store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/semantic"
)

// SetEmbedding writes a SemanticEmbedding onto the row identified by
// (namespace, name, version). Called by the indexer after the provider
// generates a fresh vector. Not part of Upsert: embeddings regenerate on a
// different cadence than spec changes (the indexer reacts to status NOTIFY,
// possibly asynchronously), and the caller already owns idempotency via the
// Checksum.
//
// Returns pkgdb.ErrNotFound if the row doesn't exist.
func (s *Store) SetEmbedding(ctx context.Context, namespace, name, version string, emb semantic.SemanticEmbedding) error {
	if namespace == "" || name == "" || version == "" {
		return errors.New("v1alpha1 store: namespace, name, and version are required")
	}
	literal, err := VectorLiteral(emb.Vector)
	if err != nil {
		return fmt.Errorf("encode vector: %w", err)
	}
	tag, err := s.pool.Exec(ctx,
		fmt.Sprintf(`
			UPDATE %s
			SET semantic_embedding              = $4::vector,
			    semantic_embedding_provider     = $5,
			    semantic_embedding_model        = $6,
			    semantic_embedding_dimensions   = $7,
			    semantic_embedding_checksum     = $8,
			    semantic_embedding_generated_at = NOW()
			WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
		namespace, name, version,
		literal,
		emb.Provider, emb.Model, emb.Dimensions, emb.Checksum,
	)
	if err != nil {
		return fmt.Errorf("set embedding: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pkgdb.ErrNotFound
	}
	return nil
}

// GetEmbeddingMetadata returns the embedding provenance columns for
// (namespace, name, version) without reading the vector itself. Used by the
// indexer to decide whether a row needs re-indexing (comparing Checksum).
//
// Returns pkgdb.ErrNotFound if the row doesn't exist. Returns (nil, nil)
// when the row exists but has no embedding yet (generated_at IS NULL).
func (s *Store) GetEmbeddingMetadata(ctx context.Context, namespace, name, version string) (*semantic.SemanticEmbeddingMetadata, error) {
	var (
		provider    *string
		model       *string
		dimensions  *int
		checksum    *string
		generatedAt *time.Time
	)
	err := s.pool.QueryRow(ctx,
		fmt.Sprintf(`
			SELECT semantic_embedding_provider, semantic_embedding_model,
			       semantic_embedding_dimensions, semantic_embedding_checksum,
			       semantic_embedding_generated_at
			FROM %s
			WHERE namespace=$1 AND name=$2 AND version=$3`, s.table),
		namespace, name, version).Scan(&provider, &model, &dimensions, &checksum, &generatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, pkgdb.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get embedding metadata: %w", err)
	}
	if generatedAt == nil {
		return nil, nil
	}
	meta := &semantic.SemanticEmbeddingMetadata{GeneratedAt: *generatedAt}
	if provider != nil {
		meta.Provider = *provider
	}
	if model != nil {
		meta.Model = *model
	}
	if dimensions != nil {
		meta.Dimensions = *dimensions
	}
	if checksum != nil {
		meta.Checksum = *checksum
	}
	return meta, nil
}

// SemanticListOpts controls a SemanticList query.
type SemanticListOpts struct {
	// Query is the embedding vector to rank results against. Must match
	// the table's column dimension.
	Query []float32
	// Threshold, when > 0, filters out rows whose cosine distance from
	// Query exceeds the threshold. 0 means no threshold filter. Values
	// ≤0.5 are typical for "roughly related"; ≤0.25 for "close match".
	Threshold float32
	// Limit caps result count. Zero means default (20).
	Limit int
	// Namespace narrows to a specific namespace; blank = cross-namespace.
	Namespace string
	// LatestOnly restricts to is_latest_version rows.
	LatestOnly bool
	// IncludeTerminating includes rows with a set DeletionTimestamp.
	IncludeTerminating bool
	// LabelSelector applies label containment as an extra predicate.
	LabelSelector map[string]string
	// ExtraWhere is the authz seam for semantic listing. Same contract
	// as ListOpts.ExtraWhere: a parameterized SQL predicate authored in
	// $1-relative numbering, rebased by the Store. Callers MUST NOT
	// interpolate untrusted input — pass via ExtraArgs.
	//
	// Without this seam, ?semantic= would bypass row-level RBAC because
	// regular ListFilter predicates only flow through ListOpts.ExtraWhere.
	ExtraWhere string
	// ExtraArgs are the bind parameters for ExtraWhere. Number of entries
	// MUST equal the distinct placeholder count in ExtraWhere.
	ExtraArgs []any
}

// SemanticList ranks rows by cosine distance (`<=>` operator) from
// opts.Query and returns them with their distance scores. Rows with
// NULL semantic_embedding are implicitly skipped (the `<=>` operator
// can't rank them).
func (s *Store) SemanticList(ctx context.Context, opts SemanticListOpts) ([]semantic.SemanticResult, error) {
	if len(opts.Query) == 0 {
		return nil, errors.New("v1alpha1 store: SemanticList requires a non-empty Query vector")
	}
	literal, err := VectorLiteral(opts.Query)
	if err != nil {
		return nil, fmt.Errorf("encode query: %w", err)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}

	args := []any{literal}
	where := []string{"semantic_embedding IS NOT NULL"}

	if opts.Namespace != "" {
		args = append(args, opts.Namespace)
		where = append(where, fmt.Sprintf("namespace = $%d", len(args)))
	}
	if opts.LatestOnly {
		where = append(where, "is_latest_version")
	}
	if !opts.IncludeTerminating {
		where = append(where, "deletion_timestamp IS NULL")
	}
	if len(opts.LabelSelector) > 0 {
		labelJSON, err := json.Marshal(opts.LabelSelector)
		if err != nil {
			return nil, fmt.Errorf("marshal labels: %w", err)
		}
		args = append(args, labelJSON)
		where = append(where, fmt.Sprintf("labels @> $%d", len(args)))
	}
	if opts.Threshold > 0 {
		args = append(args, opts.Threshold)
		where = append(where, fmt.Sprintf("semantic_embedding <=> $1::vector <= $%d", len(args)))
	}
	if opts.ExtraWhere != "" || len(opts.ExtraArgs) > 0 {
		placeholders := countDistinctPlaceholders(opts.ExtraWhere)
		if placeholders != len(opts.ExtraArgs) {
			return nil, fmt.Errorf("%w: fragment references %d distinct placeholder(s) but %d arg(s) supplied",
				ErrInvalidExtraWhere, placeholders, len(opts.ExtraArgs))
		}
		if len(opts.ExtraArgs) > 0 {
			args = append(args, opts.ExtraArgs...)
		}
		if opts.ExtraWhere != "" {
			where = append(where, rebaseSQLPlaceholders(opts.ExtraWhere, len(args)-len(opts.ExtraArgs)))
		}
	}

	args = append(args, limit)
	query := fmt.Sprintf(`
		SELECT namespace, name, version, generation, labels, annotations, spec, status,
		       deletion_timestamp, finalizers, created_at, updated_at,
		       semantic_embedding <=> $1::vector AS score
		FROM %s
		WHERE %s
		ORDER BY semantic_embedding <=> $1::vector
		LIMIT $%d`, s.table, strings.Join(where, " AND "), len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("semantic list: %w", err)
	}
	defer rows.Close()

	out := make([]semantic.SemanticResult, 0, limit)
	for rows.Next() {
		obj, score, err := scanSemanticRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, semantic.SemanticResult{Object: obj, Score: score})
	}
	return out, rows.Err()
}

// scanSemanticRow is scanRow + one trailing float column for the distance
// score. Kept separate so the regular Get/List paths don't take a hit.
func scanSemanticRow(rows pgx.Rows) (*v1alpha1.RawObject, float32, error) {
	var (
		namespace, name, version string
		generation               int64
		labelsJSON               []byte
		annotationsJSON          []byte
		specJSON                 []byte
		statusJSON               []byte
		deletionTimestamp        *time.Time
		finalizersJSON           []byte
		createdAt                time.Time
		updatedAt                time.Time
		score                    float32
	)
	if err := rows.Scan(
		&namespace, &name, &version, &generation,
		&labelsJSON, &annotationsJSON, &specJSON, &statusJSON,
		&deletionTimestamp, &finalizersJSON,
		&createdAt, &updatedAt,
		&score,
	); err != nil {
		return nil, 0, fmt.Errorf("scan semantic row: %w", err)
	}

	obj, err := decodeRow(
		namespace, name, version, generation,
		labelsJSON, annotationsJSON, specJSON, statusJSON,
		deletionTimestamp, finalizersJSON, createdAt, updatedAt,
	)
	if err != nil {
		return nil, 0, err
	}
	return obj, score, nil
}

// VectorLiteral renders a []float32 as pgvector's textual input form.
// Accepts positive / negative / zero values; rejects NaN and ±Inf because
// pgvector refuses them and a late driver-side error is harder to
// investigate than a caller-side one. Empty slice rejected — the column is
// fixed-dimension and an empty vector is always wrong.
//
// Ported verbatim from the deleted pkg/registry/database/utils/vector.go
// (removed in 2cbf1c2 / df5986c) — the format is pgvector-defined, not
// agentregistry-defined, so re-deriving wouldn't buy anything.
func VectorLiteral(vec []float32) (string, error) {
	if len(vec) == 0 {
		return "", errors.New("vector must not be empty")
	}
	var b strings.Builder
	b.Grow(len(vec) * 12)
	b.WriteByte('[')
	for i, v := range vec {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return "", fmt.Errorf("vector contains invalid value at index %d", i)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}
