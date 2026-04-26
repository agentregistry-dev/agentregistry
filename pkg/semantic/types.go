package semantic

import (
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// SemanticEmbedding is the payload written to a v1alpha1.* row's
// semantic_embedding columns. The indexer produces one of these per object
// per indexing pass; the Store persists them atomically alongside the
// provider / model / dimensions / checksum metadata that let the indexer
// skip rows whose embedding is already fresh.
type SemanticEmbedding struct {
	// Vector is the raw embedding produced by the provider. Its length
	// must equal Dimensions.
	Vector []float32
	// Provider is an identifier (e.g. "openai") recorded so later
	// indexer passes can detect provider changes.
	Provider string
	// Model is the concrete model identifier within the provider
	// (e.g. "text-embedding-3-small").
	Model string
	// Dimensions is the vector length. Must match the column type on
	// the target table (the schema fixes it at 1536 today).
	Dimensions int
	// Checksum is a stable hash over the text that produced the
	// embedding; idempotent re-indexes compare the checksum to skip
	// unchanged rows without regenerating.
	Checksum string
}

// SemanticEmbeddingMetadata is the slim view the indexer fetches to decide
// whether a row needs re-indexing. It excludes the Vector itself (cheap to
// read lots of rows) and adds the timestamp the Store stamped at write.
type SemanticEmbeddingMetadata struct {
	Provider    string
	Model       string
	Dimensions  int
	Checksum    string
	GeneratedAt time.Time
}

// SemanticResult wraps a row returned from SemanticList with its cosine
// distance from the query vector. Lower Score = closer match. Callers that
// want a relevance score can compute `1 - Score` to invert to a typical
// similarity range.
type SemanticResult struct {
	Object *v1alpha1.RawObject
	Score  float32
}
