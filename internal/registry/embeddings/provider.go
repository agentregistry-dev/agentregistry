package embeddings

import (
	"context"
	"time"
)

// Payload represents the textual input used to generate an embedding.
type Payload struct {
	Text     string
	Metadata map[string]string
}

// Result captures the vector returned by an embedding provider.
type Result struct {
	Vector      []float32
	Provider    string
	Model       string
	Dimensions  int
	GeneratedAt time.Time
}

// Provider defines the interface every embedding provider must
// implement. No first-party implementation ships in the tree right
// now — the OpenAI provider that previously lived here was removed
// when the public semantic-search surface was retired.
//
// TODO(semantic-search): when re-implementing semantic search, add a
// concrete provider (OpenAI, Voyage, sentence-transformers, etc.) and
// a Factory that constructs one from config.EmbeddingsConfig.
type Provider interface {
	Generate(ctx context.Context, payload Payload) (*Result, error)
}
