package embeddings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
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

// Provider defines the interface every embedding provider must implement.
type Provider interface {
	Generate(ctx context.Context, payload Payload) (*Result, error)
}

// Factory creates a Provider from configuration.
func Factory(cfg *config.EmbeddingsConfig, httpClient *http.Client) (Provider, error) {
	if cfg == nil || !cfg.Enabled {
		return nil, errors.New("embeddings disabled")
	}

	switch cfg.Provider {
	case "", "openai":
		return newOpenAIProvider(cfg, httpClient), nil
	default:
		return nil, fmt.Errorf("unsupported embeddings provider %q", cfg.Provider)
	}
}
