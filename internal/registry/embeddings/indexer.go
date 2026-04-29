package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// IndexOptions configures one indexing pass. The indexer iterates over
// every Kind it was constructed with and processes rows in batches.
type IndexOptions struct {
	// BatchSize controls how many rows the indexer reads per Store.List
	// page. Zero means default (100).
	BatchSize int `json:"batchSize"`
	// Force bypasses the checksum skip — every row gets a fresh
	// embedding even if its payload hasn't changed.
	Force bool `json:"force"`
	// DryRun skips Store.SetEmbedding writes; the indexer still reads
	// rows, builds payloads, and calls the provider so operators can
	// observe the operation's cost without persisting.
	DryRun bool `json:"dryRun"`
	// Kinds, when non-empty, narrows the pass to the listed Kinds
	// (e.g. []string{"MCPServer"}). Empty = all Kinds the Indexer
	// knows about.
	Kinds []string `json:"kinds,omitempty"`
	// Namespace, when non-empty, narrows the pass to a single
	// namespace. Empty = cross-namespace.
	Namespace string `json:"namespace,omitempty"`
}

// IndexStats is the per-Kind outcome of a pass. Aggregates over a single
// Kind's rows.
type IndexStats struct {
	Processed int `json:"processed"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Failures  int `json:"failures"`
}

// IndexResult is the full pass outcome. Keyed by Kind (e.g. "Agent",
// "MCPServer") — the indexer adds entries only for Kinds it actually
// processed.
type IndexResult struct {
	Stats map[string]IndexStats `json:"stats"`
}

// ProgressCallback is invoked between batches so callers (jobs.Manager,
// SSE handler) can surface progress to the client. kind is one of the
// Kind strings supplied in opts.Kinds (or the Indexer's full set).
type ProgressCallback func(kind string, stats IndexStats)

// KindBinding ties a Kind to its Store + payload builder. The indexer
// walks each binding in registration order during a pass.
type KindBinding struct {
	// Kind is the v1alpha1 Kind constant (e.g. v1alpha1.KindAgent).
	Kind string
	// Store is the generic Store bound to this Kind's table.
	Store *v1alpha1store.Store
	// BuildPayload returns the canonical embedding text for one row.
	// The RawObject carries the already-decoded metadata; the
	// callback typically unmarshals RawObject.Spec into the typed
	// Spec and hands it to one of the Build*EmbeddingPayload helpers.
	BuildPayload func(obj *v1alpha1.RawObject) (string, error)
}

// Indexer is the semantic-embedding indexer. One instance is built at
// bootstrap with the Kind bindings the server is configured to index;
// callers (HTTP handler, NOTIFY subscriber, CLI) drive it via Run.
type Indexer struct {
	bindings   []KindBinding
	provider   Provider
	dimensions int
	logger     *slog.Logger
}

// IndexerConfig carries construction parameters.
type IndexerConfig struct {
	// Bindings is the Kind → (Store, BuildPayload) registry the indexer
	// walks on every pass. Usually built from the BuiltinKinds list at
	// bootstrap via DefaultBindings, but enterprise kinds may append
	// their own.
	Bindings []KindBinding
	// Provider is the embedding generator (e.g. OpenAI).
	Provider Provider
	// Dimensions is the expected vector length. When > 0, the indexer
	// validates provider output against it so schema mismatches surface
	// at index time rather than at DB-write time.
	Dimensions int
	// Logger is optional; defaults to slog.Default().With(component="indexer").
	Logger *slog.Logger
}

// NewIndexer wires a configured Indexer. Returns an error if Bindings
// is empty or Provider is nil.
func NewIndexer(cfg IndexerConfig) (*Indexer, error) {
	if len(cfg.Bindings) == 0 {
		return nil, errors.New("indexer: at least one KindBinding is required")
	}
	if cfg.Provider == nil {
		return nil, errors.New("indexer: Provider is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default().With("component", "indexer")
	}
	return &Indexer{
		bindings:   append([]KindBinding(nil), cfg.Bindings...),
		provider:   cfg.Provider,
		dimensions: cfg.Dimensions,
		logger:     logger,
	}, nil
}

// Run executes one indexing pass. Fan-out across Kinds is sequential
// because OpenAI rate limits would serialize parallel calls anyway.
// Errors on a single row bump Failures and move on; a transport-level
// provider error halts the pass and returns a partial result.
func (i *Indexer) Run(ctx context.Context, opts IndexOptions, onProgress ProgressCallback) (*IndexResult, error) {
	res := &IndexResult{Stats: map[string]IndexStats{}}

	for _, b := range i.bindings {
		if len(opts.Kinds) > 0 && !containsString(opts.Kinds, b.Kind) {
			continue
		}
		stats, err := i.runOne(ctx, b, opts, onProgress)
		res.Stats[b.Kind] = stats
		if err != nil {
			return res, fmt.Errorf("index %s: %w", b.Kind, err)
		}
	}
	return res, nil
}

// runOne indexes a single Kind. Reads rows via Store.List in pages,
// checks each row's checksum against GetEmbeddingMetadata to skip
// already-fresh rows (unless Force), generates + writes new embeddings
// for stale ones, and reports progress after each page.
func (i *Indexer) runOne(ctx context.Context, b KindBinding, opts IndexOptions, onProgress ProgressCallback) (IndexStats, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	stats := IndexStats{}
	cursor := ""
	for {
		rows, next, err := b.Store.List(ctx, v1alpha1store.ListOpts{
			Namespace: opts.Namespace,
			Limit:     batchSize,
			Cursor:    cursor,
		})
		if err != nil {
			return stats, fmt.Errorf("list: %w", err)
		}
		for _, row := range rows {
			stats.Processed++
			if err := i.indexRow(ctx, b, row, opts, &stats); err != nil {
				i.logger.Warn("index row failed",
					"kind", b.Kind,
					"namespace", row.Metadata.Namespace,
					"name", row.Metadata.Name,
					"version", row.Metadata.Version,
					"error", err,
				)
				stats.Failures++
			}
		}
		if onProgress != nil {
			onProgress(b.Kind, stats)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	return stats, nil
}

// indexRow processes a single row. Increments the caller-owned stats
// counters (Updated / Skipped) based on outcome; Failures are counted
// by the caller on error return.
func (i *Indexer) indexRow(ctx context.Context, b KindBinding, row *v1alpha1.RawObject, opts IndexOptions, stats *IndexStats) error {
	payload, err := b.BuildPayload(row)
	if err != nil {
		return fmt.Errorf("build payload: %w", err)
	}
	if payload == "" {
		stats.Skipped++
		return nil
	}
	checksum := PayloadChecksum(payload)

	if !opts.Force {
		meta, err := b.Store.GetEmbeddingMetadata(ctx, row.Metadata.Namespace, row.Metadata.Name, row.Metadata.Version)
		if err != nil {
			return fmt.Errorf("load metadata: %w", err)
		}
		if meta != nil && meta.Checksum == checksum {
			stats.Skipped++
			return nil
		}
	}

	emb, err := GenerateSemanticEmbedding(ctx, i.provider, payload, i.dimensions)
	if err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	if opts.DryRun {
		stats.Updated++
		return nil
	}

	if err := b.Store.SetEmbedding(ctx, row.Metadata.Namespace, row.Metadata.Name, row.Metadata.Version, *emb); err != nil {
		return fmt.Errorf("set embedding: %w", err)
	}
	stats.Updated++
	return nil
}

// DefaultBindings returns the KindBindings for every v1alpha1 built-in
// kind that participates in semantic search: Agent, MCPServer, Skill,
// Prompt. Providers + Deployments are omitted (see 003 migration
// rationale). Pass the Stores map returned by
// v1alpha1store.NewStores(pool) as kindStores.
func DefaultBindings(kindStores map[string]*v1alpha1store.Store) ([]KindBinding, error) {
	required := []string{
		v1alpha1.KindAgent,
		v1alpha1.KindMCPServer,
		v1alpha1.KindSkill,
		v1alpha1.KindPrompt,
	}
	bindings := make([]KindBinding, 0, len(required))
	for _, k := range required {
		store, ok := kindStores[k]
		if !ok || store == nil {
			return nil, fmt.Errorf("indexer DefaultBindings: missing Store for kind %q", k)
		}
		bindings = append(bindings, KindBinding{
			Kind:         k,
			Store:        store,
			BuildPayload: payloadBuilderFor(k),
		})
	}
	return bindings, nil
}

// payloadBuilderFor returns the canonical text-assembly callback for a
// Kind. Each callback unmarshals the raw Spec into the typed Spec and
// calls the matching Build*EmbeddingPayload helper.
func payloadBuilderFor(kind string) func(*v1alpha1.RawObject) (string, error) {
	switch kind {
	case v1alpha1.KindAgent:
		return func(row *v1alpha1.RawObject) (string, error) {
			var spec v1alpha1.AgentSpec
			if err := json.Unmarshal(row.Spec, &spec); err != nil {
				return "", fmt.Errorf("decode agent spec: %w", err)
			}
			return BuildAgentEmbeddingPayload(row.Metadata, spec), nil
		}
	case v1alpha1.KindMCPServer:
		return func(row *v1alpha1.RawObject) (string, error) {
			var spec v1alpha1.MCPServerSpec
			if err := json.Unmarshal(row.Spec, &spec); err != nil {
				return "", fmt.Errorf("decode mcp server spec: %w", err)
			}
			return BuildMCPServerEmbeddingPayload(row.Metadata, spec), nil
		}
	case v1alpha1.KindSkill:
		return func(row *v1alpha1.RawObject) (string, error) {
			var spec v1alpha1.SkillSpec
			if err := json.Unmarshal(row.Spec, &spec); err != nil {
				return "", fmt.Errorf("decode skill spec: %w", err)
			}
			return BuildSkillEmbeddingPayload(row.Metadata, spec), nil
		}
	case v1alpha1.KindPrompt:
		return func(row *v1alpha1.RawObject) (string, error) {
			var spec v1alpha1.PromptSpec
			if err := json.Unmarshal(row.Spec, &spec); err != nil {
				return "", fmt.Errorf("decode prompt spec: %w", err)
			}
			return BuildPromptEmbeddingPayload(row.Metadata, spec), nil
		}
	default:
		return func(*v1alpha1.RawObject) (string, error) {
			return "", fmt.Errorf("no payload builder registered for kind %q", kind)
		}
	}
}

func containsString(set []string, s string) bool {
	return slices.Contains(set, s)
}
